package prover

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

const (
	// nativeProofPrivateKey is the fixed GoldenTouch mock key used only by
	// native-mode signing for deterministic local/dev regression output.
	nativeProofPrivateKey    = "92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38"
	shastaNativeMockInstance = 0xDEADC0DE
)

var anchoredEventTopic = crypto.Keccak256Hash([]byte("Anchored(uint48,uint48,bytes32)"))

type Runner interface {
	Execute(
		ctx context.Context,
		config *params.ChainConfig,
		block *types.Block,
		witness *ReplayWitness,
	) (ReplayResult, error)
}

type GethRunner struct{}

type ReplayResult struct {
	StateRoot   common.Hash
	ReceiptRoot common.Hash
	Receipts    types.Receipts
}

func (GethRunner) Execute(
	ctx context.Context,
	config *params.ChainConfig,
	block *types.Block,
	witness *ReplayWitness,
) (ReplayResult, error) {
	memdb := witness.Witness.MakeHashDB()
	db, err := state.New(
		witness.Witness.Root(),
		state.NewDatabase(triedb.NewDatabase(memdb, triedb.HashDefaults), state.NewCodeDB(memdb)),
	)
	if err != nil {
		return ReplayResult{}, err
	}

	executionBlock, expectedDifficulty := replayExecutionBlock(config, block)
	chain := newReplayChainContext(config, executionBlock, witness)
	validator := core.NewBlockValidator(config, nil)

	res, err := processReplayBlock(ctx, chain, config, executionBlock, expectedDifficulty, db, vm.Config{})
	if err != nil {
		return ReplayResult{}, err
	}
	if err := validator.ValidateState(executionBlock, db, res, true); err != nil {
		return ReplayResult{}, err
	}
	if err := validateReplayRequestsHash(executionBlock.Header(), res.Requests); err != nil {
		return ReplayResult{}, err
	}

	receiptRoot := types.DeriveSha(res.Receipts, trie.NewStackTrie(nil))
	stateRoot := db.IntermediateRoot(config.IsEIP158(executionBlock.Number()))
	return ReplayResult{
		StateRoot:   stateRoot,
		ReceiptRoot: receiptRoot,
		Receipts:    res.Receipts,
	}, nil
}

func processReplayBlock(
	ctx context.Context,
	chain *replayChainContext,
	config *params.ChainConfig,
	block *types.Block,
	expectedDifficulty *big.Int,
	statedb *state.StateDB,
	cfg vm.Config,
) (*core.ProcessResult, error) {
	if config == nil || !config.IsUnzen(block.Time()) {
		return core.NewStateProcessor(chain).Process(ctx, block, statedb, cfg)
	}
	return processUnzenReplayBlock(ctx, chain, config, block, expectedDifficulty, statedb, cfg)
}

func processUnzenReplayBlock(
	_ context.Context,
	chain *replayChainContext,
	config *params.ChainConfig,
	block *types.Block,
	expectedDifficulty *big.Int,
	statedb *state.StateDB,
	cfg vm.Config,
) (*core.ProcessResult, error) {
	var (
		receipts    types.Receipts
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = core.NewGasPool(block.GasLimit())
	)
	var tracingStateDB = vm.StateDB(statedb)
	if hooks := cfg.Tracer; hooks != nil {
		tracingStateDB = state.NewHookedState(statedb, hooks)
	}

	if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(tracingStateDB)
	}

	signer := types.MakeSigner(config, header.Number, header.Time)
	cfg.ZkGasMeter = vm.NewZkGasMeter(unzenZkGasScheduleFor(config))
	for i, tx := range block.Transactions() {
		if tx.Type() == types.BlobTxType {
			return nil, fmt.Errorf("blob transaction at index %d not allowed in Unzen block", i)
		}
	}

	blockContext := core.NewEVMBlockContext(header, chain, nil)
	evm := vm.NewEVM(blockContext, tracingStateDB, config, cfg)

	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		core.ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if config.IsPrague(block.Number(), block.Time()) || config.IsVerkle(block.Number(), block.Time()) {
		core.ProcessParentBlockHash(block.ParentHash(), evm)
	}

	for i, tx := range block.Transactions() {
		if i == 0 && config.Taiko {
			if err := tx.MarkAsAnchor(); err != nil {
				return nil, err
			}
		}
		msg, err := core.TransactionToMessage(tx, signer, header.BaseFee)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		if config.IsShasta(header.Time) {
			msg.BasefeeSharingPctg = core.DecodeShastaBasefeeSharingPctg(header.Extra)
		} else if config.IsOntake(block.Number()) {
			msg.BasefeeSharingPctg = core.DecodeOntakeExtraData(header.Extra)
		}
		statedb.SetTxContext(tx.Hash(), i)

		cfg.ZkGasMeter.ResetTransaction()
		evm.ResetZkGasErr()
		receipt, err := core.ApplyTransactionWithEVM(msg, gp, statedb, blockNumber, blockHash, blockContext.Time, tx, evm)
		if err != nil {
			if errors.Is(err, vm.ErrZkGasLimitExceeded) && i > 0 {
				cfg.ZkGasMeter.ResetTransaction()
				evm.ResetZkGasErr()
				break
			}
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}

		if commitErr := cfg.ZkGasMeter.CommitTransaction(); commitErr != nil && i > 0 {
			cfg.ZkGasMeter.ResetTransaction()
			break
		}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}

	requests, err := replayPostExecution(config, block, allLogs, evm)
	if err != nil {
		return nil, err
	}
	if len(block.Transactions()) != len(receipts) {
		return nil, fmt.Errorf(
			"Unzen block body extends past zk gas truncation point: body has %d transactions but execution committed %d",
			len(block.Transactions()),
			len(receipts),
		)
	}

	recomputed := new(big.Int).SetUint64(cfg.ZkGasMeter.BlockZkGasUsed())
	if expectedDifficulty == nil || expectedDifficulty.Cmp(recomputed) != 0 {
		return nil, fmt.Errorf("zk gas difficulty mismatch: header has %v, recomputed %v", expectedDifficulty, recomputed)
	}

	chain.Engine().Finalize(chain, header, tracingStateDB, block.Body())

	return &core.ProcessResult{
		Receipts: receipts,
		Requests: requests,
		Logs:     allLogs,
		GasUsed:  gp.Used(),
	}, nil
}

func replayPostExecution(
	config *params.ChainConfig,
	block *types.Block,
	allLogs []*types.Log,
	evm *vm.EVM,
) ([][]byte, error) {
	if !config.IsPrague(block.Number(), block.Time()) {
		return nil, nil
	}

	requests := [][]byte{}
	if err := core.ParseDepositLogs(&requests, allLogs, config); err != nil {
		return requests, fmt.Errorf("failed to parse deposit logs: %w", err)
	}
	if err := core.ProcessWithdrawalQueue(&requests, evm); err != nil {
		return requests, fmt.Errorf("failed to process withdrawal queue: %w", err)
	}
	if err := core.ProcessConsolidationQueue(&requests, evm); err != nil {
		return requests, fmt.Errorf("failed to process consolidation queue: %w", err)
	}
	return requests, nil
}

func replayExecutionBlock(config *params.ChainConfig, block *types.Block) (*types.Block, *big.Int) {
	if config == nil || !config.IsUnzen(block.Time()) {
		return block, nil
	}
	header := types.CopyHeader(block.Header())
	expectedDifficulty := new(big.Int)
	if header.Difficulty != nil {
		expectedDifficulty.Set(header.Difficulty)
	}
	header.Difficulty = common.Big0
	return types.NewBlockWithHeader(header).WithBody(*block.Body()), expectedDifficulty
}

type ReplayService struct {
	runner Runner
	signer ProofSigner
	// aggregateEnabled gates the /prove/shasta-aggregate endpoint, which signs
	// the final on-chain digest without executing any blocks. In native mode the
	// signing key is the published mock key, so the endpoint is a proof-forgery
	// oracle; NewConfiguredReplayService only enables it in TEE mode or when
	// native mode is explicitly opted into dev mode. The test/default
	// constructors leave it enabled.
	aggregateEnabled bool
}

func NewReplayService(runner Runner) ReplayService {
	return newReplayService(runner, NewNativeProofSigner(shastaNativeMockInstance), true)
}

func newReplayService(runner Runner, signer ProofSigner, aggregateEnabled bool) ReplayService {
	if runner == nil {
		runner = GethRunner{}
	}
	if signer == nil {
		signer = NewNativeProofSigner(shastaNativeMockInstance)
	}
	return ReplayService{
		runner:           runner,
		signer:           signer,
		aggregateEnabled: aggregateEnabled,
	}
}

func (s ReplayService) Prove(
	ctx context.Context,
	req *ValidatedRequest,
) (protocol.ProofResult, error) {
	if req == nil {
		return protocol.ProofResult{}, fmt.Errorf("validated request is nil")
	}
	if err := validateRequestSigningBinding(req); err != nil {
		return protocol.ProofResult{}, err
	}
	if len(req.Blocks) == 0 || len(req.Request.Payload.Blocks) == 0 {
		return protocol.ProofResult{}, fmt.Errorf("validated request must include at least one replay block")
	}
	if len(req.Blocks) != len(req.Request.Payload.Blocks) {
		return protocol.ProofResult{}, fmt.Errorf(
			"validated replay block count mismatch: views=%d raw=%d",
			len(req.Blocks),
			len(req.Request.Payload.Blocks),
		)
	}
	config, err := chainConfigFor(req.Carry.ChainID)
	if err != nil {
		return protocol.ProofResult{}, err
	}

	var previousAnchor uint64
	for index, replay := range req.Request.Payload.Blocks {
		block, witness, err := decodeReplayBlock(replay)
		if err != nil {
			return protocol.ProofResult{}, fmt.Errorf("decode replay block %d: %w", index, err)
		}
		if err := validateReplayBlockMatchesView(block, req.Blocks[index], index); err != nil {
			return protocol.ProofResult{}, err
		}
		if err := validateReplayWitness(block, witness); err != nil {
			return protocol.ProofResult{}, fmt.Errorf("validate replay block %d: %w", index, err)
		}
		if err := validateReplayBlockBody(block); err != nil {
			return protocol.ProofResult{}, fmt.Errorf("validate replay block %d body: %w", index, err)
		}

		result, err := s.runner.Execute(ctx, config, blockForStatelessExecution(block), witness)
		if err != nil {
			return protocol.ProofResult{}, fmt.Errorf("replay block %d: %w", index, err)
		}
		if result.StateRoot != block.Root() {
			return protocol.ProofResult{}, fmt.Errorf(
				"block %d state root mismatch: got %s expected %s",
				block.NumberU64(),
				result.StateRoot,
				block.Root(),
			)
		}
		if result.ReceiptRoot != block.ReceiptHash() {
			return protocol.ProofResult{}, fmt.Errorf(
				"block %d receipt root mismatch: got %s expected %s",
				block.NumberU64(),
				result.ReceiptRoot,
				block.ReceiptHash(),
			)
		}
		if req.validatedGuestInput {
			if index == 0 {
				previousAnchor, err = validateFirstReplayAnchorEvent(req, result.Receipts)
			} else {
				previousAnchor, err = validateSubsequentReplayAnchorEvent(
					req.Carry.ChainID,
					previousAnchor,
					result.Receipts,
				)
			}
			if err != nil {
				return protocol.ProofResult{}, fmt.Errorf("replay block %d anchor event: %w", index, err)
			}
		}
	}

	if err := validateBlockViews(req.Blocks, req.Carry); err != nil {
		return protocol.ProofResult{}, fmt.Errorf("validated carry block binding: %w", err)
	}
	inputHash := hashShastaSubproofCarry(req.Carry)
	return buildProofResult(inputHash, s.signer)
}

func validateReplayBlockMatchesView(block *types.Block, view BlockView, index int) error {
	if block.NumberU64() != view.Number {
		return fmt.Errorf(
			"validated replay block %d number mismatch: raw=%d view=%d",
			index,
			block.NumberU64(),
			view.Number,
		)
	}
	if block.Hash() != view.Hash {
		return fmt.Errorf(
			"validated replay block %d hash mismatch: raw=%s view=%s",
			index,
			block.Hash().Hex(),
			view.Hash.Hex(),
		)
	}
	if block.ParentHash() != view.ParentHash {
		return fmt.Errorf(
			"validated replay block %d parent hash mismatch: raw=%s view=%s",
			index,
			block.ParentHash().Hex(),
			view.ParentHash.Hex(),
		)
	}
	if block.Root() != view.StateRoot {
		return fmt.Errorf(
			"validated replay block %d state root mismatch: raw=%s view=%s",
			index,
			block.Root().Hex(),
			view.StateRoot.Hex(),
		)
	}
	if block.ReceiptHash() != view.ReceiptsRoot {
		return fmt.Errorf(
			"validated replay block %d receipt root mismatch: raw=%s view=%s",
			index,
			block.ReceiptHash().Hex(),
			view.ReceiptsRoot.Hex(),
		)
	}
	return nil
}

func validateFirstReplayAnchorEvent(req *ValidatedRequest, receipts types.Receipts) (uint64, error) {
	if req.Request.Payload.GuestInput == nil {
		return 0, nil
	}
	lastAnchor, err := decodeGuestInputLastAnchorBlockNumber(req.Request.Payload.GuestInput.Taiko)
	if err != nil {
		return 0, err
	}
	if lastAnchor == nil {
		return 0, fmt.Errorf("missing taiko.prover_data.last_anchor_block_number")
	}

	prevAnchor, anchor, err := replayAnchorTransition(req.Carry.ChainID, receipts)
	if err != nil {
		return 0, err
	}
	if prevAnchor != *lastAnchor {
		return 0, fmt.Errorf(
			"prover_data.last_anchor_block_number mismatch: expected %d (first Anchor event prevAnchorBlockNumber), got %d",
			prevAnchor,
			*lastAnchor,
		)
	}
	return anchor, nil
}

func validateSubsequentReplayAnchorEvent(
	chainID uint64,
	expectedPreviousAnchor uint64,
	receipts types.Receipts,
) (uint64, error) {
	prevAnchor, anchor, err := replayAnchorTransition(chainID, receipts)
	if err != nil {
		return 0, err
	}
	if prevAnchor != expectedPreviousAnchor {
		return 0, fmt.Errorf(
			"anchor continuity mismatch: expected previous anchor %d got %d",
			expectedPreviousAnchor,
			prevAnchor,
		)
	}
	return anchor, nil
}

func replayAnchorTransition(chainID uint64, receipts types.Receipts) (uint64, uint64, error) {
	if len(receipts) == 0 || receipts[0] == nil {
		return 0, 0, fmt.Errorf("missing first transaction receipt")
	}
	if err := validateSuccessfulAnchorReceipt(receipts[0]); err != nil {
		return 0, 0, err
	}
	return replayAnchoredEvent(chainID, receipts[0].Logs)
}

func validateSuccessfulAnchorReceipt(receipt *types.Receipt) error {
	if receipt == nil {
		return fmt.Errorf("missing anchor transaction receipt")
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("anchor transaction receipt failed")
	}
	return nil
}

func replayAnchoredEvent(chainID uint64, logs []*types.Log) (uint64, uint64, error) {
	anchorAddress, err := shastaTaikoL2Address(chainID)
	if err != nil {
		return 0, 0, err
	}

	var prevAnchor uint64
	var anchor uint64
	found := false
	for _, log := range logs {
		if log == nil || log.Address != anchorAddress || len(log.Topics) == 0 || log.Topics[0] != anchoredEventTopic {
			continue
		}
		if found {
			return 0, 0, fmt.Errorf("multiple Anchored events from TaikoL2")
		}
		if len(log.Topics) != 1 {
			return 0, 0, fmt.Errorf("malformed Anchored event topics")
		}
		if len(log.Data) != 96 {
			return 0, 0, fmt.Errorf("malformed Anchored event data length %d", len(log.Data))
		}
		prevAnchor, err = uint48FromEventWord(log.Data[:32])
		if err != nil {
			return 0, 0, fmt.Errorf("decode prevAnchorBlockNumber: %w", err)
		}
		anchor, err = uint48FromEventWord(log.Data[32:64])
		if err != nil {
			return 0, 0, fmt.Errorf("decode anchorBlockNumber: %w", err)
		}
		found = true
	}
	if !found {
		return 0, 0, fmt.Errorf("missing Anchored event from TaikoL2")
	}
	return prevAnchor, anchor, nil
}

func uint48FromEventWord(word []byte) (uint64, error) {
	value := new(big.Int).SetBytes(word)
	if !value.IsUint64() || value.Uint64() > uint64(1<<48)-1 {
		return 0, fmt.Errorf("value exceeds uint48")
	}
	return value.Uint64(), nil
}

func validateReplayWitness(block *types.Block, witness *ReplayWitness) error {
	if len(witness.Witness.Headers) == 0 {
		return fmt.Errorf("witness must include a full parent header")
	}
	if block.NumberU64() == 0 {
		return fmt.Errorf("genesis block replay is not supported")
	}

	parent := witness.Witness.Headers[0]
	if parent.Number == nil {
		return fmt.Errorf("parent header is missing number")
	}
	if parent.Number.Uint64()+1 != block.NumberU64() {
		return fmt.Errorf(
			"parent header number mismatch: got %d expected %d",
			parent.Number.Uint64(),
			block.NumberU64()-1,
		)
	}
	if parent.Hash() != block.ParentHash() {
		return fmt.Errorf(
			"parent header hash mismatch: got %s expected %s",
			parent.Hash().Hex(),
			block.ParentHash().Hex(),
		)
	}
	if witness.Witness.Root() != parent.Root {
		return fmt.Errorf(
			"witness root mismatch: got %s expected %s",
			witness.Witness.Root().Hex(),
			parent.Root.Hex(),
		)
	}
	for index := 1; index < len(witness.Witness.Headers); index++ {
		newer := witness.Witness.Headers[index-1]
		older := witness.Witness.Headers[index]
		if older.Number == nil {
			return fmt.Errorf("ancestor header %d is missing number", index)
		}
		if newer.Number.Uint64() != older.Number.Uint64()+1 {
			return fmt.Errorf(
				"ancestor header %d number mismatch: got %d expected %d",
				index,
				older.Number.Uint64(),
				newer.Number.Uint64()-1,
			)
		}
		if newer.ParentHash != older.Hash() {
			return fmt.Errorf(
				"ancestor header %d hash mismatch: got %s expected %s",
				index,
				older.Hash().Hex(),
				newer.ParentHash.Hex(),
			)
		}
	}
	return nil
}

func validateReplayBlockBody(block *types.Block) error {
	header := block.Header()
	if hash := types.CalcUncleHash(block.Uncles()); hash != header.UncleHash {
		return fmt.Errorf(
			"ommer hash mismatch: got %s expected %s",
			hash.Hex(),
			header.UncleHash.Hex(),
		)
	}
	if hash := types.DeriveSha(block.Transactions(), trie.NewStackTrie(nil)); hash != header.TxHash {
		return fmt.Errorf(
			"transaction root mismatch: got %s expected %s",
			hash.Hex(),
			header.TxHash.Hex(),
		)
	}
	if header.WithdrawalsHash != nil {
		if block.Withdrawals() == nil {
			return fmt.Errorf("missing withdrawals in block body")
		}
		if hash := types.DeriveSha(block.Withdrawals(), trie.NewStackTrie(nil)); hash != *header.WithdrawalsHash {
			return fmt.Errorf(
				"withdrawals root mismatch: got %s expected %s",
				hash.Hex(),
				header.WithdrawalsHash.Hex(),
			)
		}
	} else if block.Withdrawals() != nil {
		return fmt.Errorf("withdrawals present in block body without header commitment")
	}
	return nil
}

func validateReplayRequestsHash(header *types.Header, requests [][]byte) error {
	if header.RequestsHash == nil {
		if requests != nil {
			return fmt.Errorf("requests present in block body without header commitment")
		}
		return nil
	}

	hash := types.CalcRequestsHash(requests)
	if hash != *header.RequestsHash {
		return fmt.Errorf(
			"requests hash mismatch: got %s expected %s",
			hash.Hex(),
			header.RequestsHash.Hex(),
		)
	}
	return nil
}

func decodeReplayBlock(replay protocol.ReplayBlock) (*types.Block, *ReplayWitness, error) {
	decoded, err := decodeBlockEnvelope(replay.Block)
	if err != nil {
		return nil, nil, err
	}
	header, err := decodeHeader(decoded.Header)
	if err != nil {
		return nil, nil, err
	}
	txs, err := decodeTransactions(decoded.Body.Transactions)
	if err != nil {
		return nil, nil, err
	}
	uncles := make([]*types.Header, len(decoded.Body.Ommers))
	for i, rawUncle := range decoded.Body.Ommers {
		uncle, err := decodeHeader(rawUncle)
		if err != nil {
			return nil, nil, fmt.Errorf("decode ommer %d: %w", i, err)
		}
		uncles[i] = uncle
	}
	withdrawals, err := decodeWithdrawals(decoded.Body.Withdrawals)
	if err != nil {
		return nil, nil, err
	}

	block := types.NewBlockWithHeader(header).WithBody(types.Body{
		Transactions: txs,
		Uncles:       uncles,
		Withdrawals:  withdrawals,
	})
	witness, err := decodeWitness(replay.Witness)
	if err != nil {
		return nil, nil, err
	}

	return block, witness, nil
}

type replayChainContext struct {
	config         *params.ChainConfig
	engine         consensus.Engine
	current        *types.Header
	headersByHash  map[common.Hash]*types.Header
	hashesByNumber map[uint64]common.Hash
}

func newReplayChainContext(
	config *params.ChainConfig,
	block *types.Block,
	witness *ReplayWitness,
) *replayChainContext {
	ctx := &replayChainContext{
		config:         config,
		engine:         beacon.New(ethash.NewFaker()),
		current:        block.Header(),
		headersByHash:  make(map[common.Hash]*types.Header, len(witness.Witness.Headers)),
		hashesByNumber: make(map[uint64]common.Hash, len(witness.Witness.Headers)),
	}
	for _, header := range witness.Witness.Headers {
		ctx.addHeader(header.Hash(), header)
	}
	return ctx
}

func (c *replayChainContext) addHeader(hash common.Hash, header *types.Header) {
	c.headersByHash[hash] = types.CopyHeader(header)
	c.hashesByNumber[header.Number.Uint64()] = hash
}

func (c *replayChainContext) Config() *params.ChainConfig {
	return c.config
}

func (c *replayChainContext) CurrentHeader() *types.Header {
	return c.current
}

func (c *replayChainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	header, ok := c.headersByHash[hash]
	if !ok || header.Number == nil || header.Number.Uint64() != number {
		return nil
	}
	return types.CopyHeader(header)
}

func (c *replayChainContext) GetHeaderByNumber(number uint64) *types.Header {
	hash, ok := c.hashesByNumber[number]
	if !ok {
		return nil
	}
	return c.GetHeader(hash, number)
}

func (c *replayChainContext) GetHeaderByHash(hash common.Hash) *types.Header {
	header, ok := c.headersByHash[hash]
	if !ok {
		return nil
	}
	return types.CopyHeader(header)
}

func (c *replayChainContext) Engine() consensus.Engine {
	return c.engine
}

func blockForStatelessExecution(block *types.Block) *types.Block {
	header := block.Header()
	header.Root = common.Hash{}
	header.ReceiptHash = common.Hash{}
	return types.NewBlockWithHeader(header).WithBody(*block.Body())
}

func chainConfigFor(chainID uint64) (*params.ChainConfig, error) {
	switch chainID {
	case 0, params.MainnetChainConfig.ChainID.Uint64():
		return cloneChainConfig(params.MainnetChainConfig), nil
	case params.HoleskyChainConfig.ChainID.Uint64():
		return cloneChainConfig(params.HoleskyChainConfig), nil
	case params.SepoliaChainConfig.ChainID.Uint64():
		return cloneChainConfig(params.SepoliaChainConfig), nil
	case params.HoodiChainConfig.ChainID.Uint64():
		return cloneChainConfig(params.HoodiChainConfig), nil
	case params.TaikoMainnetNetworkID.Uint64():
		cfg := cloneChainConfig(params.TaikoChainConfig)
		cfg.ChainID = bigIntFromUint64(chainID)
		cfg.OntakeBlock = cloneBigInt(core.MainnetOntakeBlock)
		cfg.PacayaBlock = cloneBigInt(core.MainnetPacayaBlock)
		cfg.ShastaTime = cloneUint64(core.MainnetShastaTime)
		enableUnzenForksFrom(cfg, core.MainnetUnzenTime)
		return cfg, nil
	case params.TaikoInternalNetworkID.Uint64():
		cfg := cloneChainConfig(params.TaikoChainConfig)
		cfg.ChainID = bigIntFromUint64(chainID)
		cfg.OntakeBlock = cloneBigInt(core.InternalDevnetOntakeBlock)
		cfg.PacayaBlock = cloneBigInt(core.InternalDevnetPacayaBlock)
		cfg.ShastaTime = cloneUint64(core.InternalShastaTime)
		enableUnzenForksFrom(cfg, core.DevnetUnzenTime)
		return cfg, nil
	case params.MasayaDevnetNetworkID.Uint64():
		cfg := cloneChainConfig(params.TaikoChainConfig)
		cfg.ChainID = bigIntFromUint64(chainID)
		cfg.OntakeBlock = cloneBigInt(core.MasayaDevnetOntakeBlock)
		cfg.PacayaBlock = cloneBigInt(core.MasayaDevnetPacayaBlock)
		cfg.ShastaTime = cloneUint64(core.MasayaShastaTime)
		enableUnzenForksFrom(cfg, core.MasayaUnzenTime)
		return cfg, nil
	case params.TaikoHoodiNetworkID.Uint64():
		cfg := cloneChainConfig(params.TaikoChainConfig)
		cfg.ChainID = bigIntFromUint64(chainID)
		cfg.OntakeBlock = cloneBigInt(core.TaikoHoodiOntakeBlock)
		cfg.PacayaBlock = cloneBigInt(core.TaikoHoodiPacayaBlock)
		cfg.ShastaTime = cloneUint64(core.HoodiShastaTime)
		enableUnzenForksFrom(cfg, core.HoodiUnzenTime)
		return cfg, nil
	default:
		return nil, fmt.Errorf("unsupported chain ID: %d", chainID)
	}
}

func enableUnzenForksFrom(cfg *params.ChainConfig, timestamp uint64) {
	if cfg == nil {
		return
	}
	if cfg.UnzenTime == nil {
		cfg.UnzenTime = cloneUint64(timestamp)
	}
	if cfg.CancunTime == nil {
		cfg.CancunTime = cloneUint64(timestamp)
	}
	if cfg.PragueTime == nil {
		cfg.PragueTime = cloneUint64(timestamp)
	}
	if cfg.OsakaTime == nil {
		cfg.OsakaTime = cloneUint64(timestamp)
	}
	if cfg.BlobScheduleConfig == nil {
		cfg.BlobScheduleConfig = cloneBlobSchedule(params.DefaultBlobSchedule)
	}
}

func unzenZkGasScheduleFor(config *params.ChainConfig) *vm.ZkGasSchedule {
	// Current taiko-geth no longer selects Unzen zk-gas schedules by chain ID.
	// In particular, taiko-geth #569 reset Masaya to the default Unzen schedule.
	_ = config
	return &vm.UnzenZkGasSchedule
}

func cloneChainConfig(cfg *params.ChainConfig) *params.ChainConfig {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	cloned.ChainID = cloneBigInt(cfg.ChainID)
	cloned.HomesteadBlock = cloneBigInt(cfg.HomesteadBlock)
	cloned.DAOForkBlock = cloneBigInt(cfg.DAOForkBlock)
	cloned.EIP150Block = cloneBigInt(cfg.EIP150Block)
	cloned.EIP155Block = cloneBigInt(cfg.EIP155Block)
	cloned.EIP158Block = cloneBigInt(cfg.EIP158Block)
	cloned.ByzantiumBlock = cloneBigInt(cfg.ByzantiumBlock)
	cloned.ConstantinopleBlock = cloneBigInt(cfg.ConstantinopleBlock)
	cloned.PetersburgBlock = cloneBigInt(cfg.PetersburgBlock)
	cloned.IstanbulBlock = cloneBigInt(cfg.IstanbulBlock)
	cloned.MuirGlacierBlock = cloneBigInt(cfg.MuirGlacierBlock)
	cloned.BerlinBlock = cloneBigInt(cfg.BerlinBlock)
	cloned.LondonBlock = cloneBigInt(cfg.LondonBlock)
	cloned.ArrowGlacierBlock = cloneBigInt(cfg.ArrowGlacierBlock)
	cloned.GrayGlacierBlock = cloneBigInt(cfg.GrayGlacierBlock)
	cloned.MergeNetsplitBlock = cloneBigInt(cfg.MergeNetsplitBlock)
	cloned.ShanghaiTime = cloneUint64Ptr(cfg.ShanghaiTime)
	cloned.CancunTime = cloneUint64Ptr(cfg.CancunTime)
	cloned.PragueTime = cloneUint64Ptr(cfg.PragueTime)
	cloned.OsakaTime = cloneUint64Ptr(cfg.OsakaTime)
	cloned.BPO1Time = cloneUint64Ptr(cfg.BPO1Time)
	cloned.BPO2Time = cloneUint64Ptr(cfg.BPO2Time)
	cloned.BPO3Time = cloneUint64Ptr(cfg.BPO3Time)
	cloned.BPO4Time = cloneUint64Ptr(cfg.BPO4Time)
	cloned.BPO5Time = cloneUint64Ptr(cfg.BPO5Time)
	cloned.AmsterdamTime = cloneUint64Ptr(cfg.AmsterdamTime)
	cloned.VerkleTime = cloneUint64Ptr(cfg.VerkleTime)
	cloned.TerminalTotalDifficulty = cloneBigInt(cfg.TerminalTotalDifficulty)
	cloned.Ethash = cloneEthash(cfg.Ethash)
	cloned.Clique = cloneClique(cfg.Clique)
	cloned.BlobScheduleConfig = cloneBlobSchedule(cfg.BlobScheduleConfig)
	cloned.OntakeBlock = cloneBigInt(cfg.OntakeBlock)
	cloned.PacayaBlock = cloneBigInt(cfg.PacayaBlock)
	cloned.ShastaTime = cloneUint64Ptr(cfg.ShastaTime)
	cloned.UnzenTime = cloneUint64Ptr(cfg.UnzenTime)
	return &cloned
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func bigIntFromUint64(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}

func cloneUint64(value uint64) *uint64 {
	return &value
}

func cloneUint64Ptr(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneEthash(value *params.EthashConfig) *params.EthashConfig {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneClique(value *params.CliqueConfig) *params.CliqueConfig {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBlobSchedule(value *params.BlobScheduleConfig) *params.BlobScheduleConfig {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Cancun = cloneBlobConfig(value.Cancun)
	cloned.Prague = cloneBlobConfig(value.Prague)
	cloned.Osaka = cloneBlobConfig(value.Osaka)
	cloned.Verkle = cloneBlobConfig(value.Verkle)
	cloned.BPO1 = cloneBlobConfig(value.BPO1)
	cloned.BPO2 = cloneBlobConfig(value.BPO2)
	cloned.BPO3 = cloneBlobConfig(value.BPO3)
	cloned.BPO4 = cloneBlobConfig(value.BPO4)
	cloned.BPO5 = cloneBlobConfig(value.BPO5)
	cloned.Amsterdam = cloneBlobConfig(value.Amsterdam)
	return &cloned
}

func cloneBlobConfig(value *params.BlobConfig) *params.BlobConfig {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
