package prover

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

func validateManifestTransactionRoot(
	view *GuestInputView,
	canonicalBlock *types.Block,
	witness *ReplayWitness,
	manifestTxs types.Transactions,
) error {
	filteredTxs, err := filterManifestTransactions(
		context.Background(),
		view.GuestInputChainID,
		canonicalBlock,
		manifestTxs,
		witness,
	)
	if err != nil {
		return err
	}

	header := canonicalBlock.Header()
	computedRoot := types.DeriveSha(filteredTxs, trie.NewStackTrie(nil))
	if computedRoot != header.TxHash {
		return fmt.Errorf(
			"transaction root mismatch: expected %s got %s",
			header.TxHash.Hex(),
			computedRoot.Hex(),
		)
	}
	return nil
}

func filterManifestTransactions(
	ctx context.Context,
	chainID uint64,
	canonicalBlock *types.Block,
	manifestTxs types.Transactions,
	witness *ReplayWitness,
) (types.Transactions, error) {
	if witness == nil || witness.Witness == nil {
		return nil, fmt.Errorf("missing witness for manifest transaction filtering")
	}
	canonicalTxs := canonicalBlock.Transactions()
	if len(canonicalTxs) == 0 {
		return nil, fmt.Errorf("missing anchor transaction")
	}

	config, err := chainConfigFor(chainID)
	if err != nil {
		return nil, err
	}

	memdb := witness.Witness.MakeHashDB()
	statedb, err := state.New(
		witness.Witness.Root(),
		state.NewDatabase(triedb.NewDatabase(memdb, triedb.HashDefaults), state.NewCodeDB(memdb)),
	)
	if err != nil {
		return nil, fmt.Errorf("open witness state: %w", err)
	}

	executionBlock, _ := replayExecutionBlock(config, canonicalBlock)
	chain := newReplayChainContext(config, executionBlock, witness)
	candidates, err := manifestCandidateTransactions(config, executionBlock.Header(), canonicalTxs[0], manifestTxs)
	if err != nil {
		return nil, err
	}

	committedTxs, err := commitFilteredManifestTransactions(
		ctx,
		chain,
		config,
		executionBlock,
		statedb,
		candidates,
	)
	return committedTxs, err
}

func manifestCandidateTransactions(
	config *params.ChainConfig,
	header *types.Header,
	anchor *types.Transaction,
	manifestTxs types.Transactions,
) (types.Transactions, error) {
	anchorCopy, err := cloneTransaction(anchor)
	if err != nil {
		return nil, fmt.Errorf("clone anchor transaction: %w", err)
	}

	signer := types.MakeSigner(config, header.Number, header.Time)
	candidates := make(types.Transactions, 0, 1+len(manifestTxs))
	candidates = append(candidates, anchorCopy)
	for _, tx := range manifestTxs {
		if tx.Type() == types.BlobTxType {
			continue
		}
		if _, err := types.Sender(signer, tx); err != nil {
			continue
		}
		candidates = append(candidates, tx)
	}
	return candidates, nil
}

func commitFilteredManifestTransactions(
	ctx context.Context,
	chain *replayChainContext,
	config *params.ChainConfig,
	block *types.Block,
	statedb *state.StateDB,
	candidates types.Transactions,
) (types.Transactions, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("missing anchor transaction")
	}

	var (
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		gp          = core.NewGasPool(block.GasLimit())
	)
	tracingStateDB := vm.StateDB(statedb)

	if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(tracingStateDB)
	}

	signer := types.MakeSigner(config, header.Number, header.Time)
	cfg := vm.Config{}
	isUnzen := config.IsUnzen(block.Time())
	if isUnzen {
		cfg.ZkGasMeter = vm.NewZkGasMeter(unzenZkGasScheduleFor(config))
	}

	blockContext := core.NewEVMBlockContext(header, chain, nil)
	evm := vm.NewEVM(blockContext, tracingStateDB, config, cfg)
	if isUnzen {
		evm.SetZkGasMeter(cfg.ZkGasMeter)
	}

	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		core.ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if config.IsPrague(block.Number(), block.Time()) || config.IsVerkle(block.Number(), block.Time()) {
		core.ProcessParentBlockHash(block.ParentHash(), evm)
	}

	committed := make(types.Transactions, 0, len(candidates))
	for index, tx := range candidates {
		txCopy := tx
		if index == 0 {
			var err error
			txCopy, err = cloneTransaction(tx)
			if err != nil {
				return nil, fmt.Errorf("clone anchor transaction: %w", err)
			}
			if err := txCopy.MarkAsAnchor(); err != nil {
				return nil, fmt.Errorf("mark anchor transaction: %w", err)
			}
		}

		msg, err := core.TransactionToMessage(txCopy, signer, header.BaseFee)
		if err != nil {
			if index == 0 {
				return nil, fmt.Errorf("anchor transaction: %w", err)
			}
			continue
		}
		if config.IsShasta(header.Time) {
			msg.BasefeeSharingPctg = core.DecodeShastaBasefeeSharingPctg(header.Extra)
		} else if config.IsOntake(blockNumber) {
			msg.BasefeeSharingPctg = core.DecodeOntakeExtraData(header.Extra)
		}

		statedb.SetTxContext(txCopy.Hash(), len(committed))
		if isUnzen {
			cfg.ZkGasMeter.ResetTransaction()
			evm.ResetZkGasErr()
		}

		_, err = core.ApplyTransactionWithEVM(msg, gp, statedb, blockNumber, blockHash, blockContext.Time, txCopy, evm)
		if err != nil {
			if isUnzen && errors.Is(err, vm.ErrZkGasLimitExceeded) && index > 0 {
				cfg.ZkGasMeter.ResetTransaction()
				evm.ResetZkGasErr()
				break
			}
			if index == 0 {
				return nil, fmt.Errorf("anchor transaction failed: %w", err)
			}
			continue
		}

		if isUnzen {
			if commitErr := cfg.ZkGasMeter.CommitTransaction(); commitErr != nil && index > 0 {
				cfg.ZkGasMeter.ResetTransaction()
				break
			}
		}

		committed = append(committed, tx)
	}

	return committed, nil
}

func cloneTransaction(tx *types.Transaction) (*types.Transaction, error) {
	raw, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var cloned types.Transaction
	if err := cloned.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	return &cloned, nil
}
