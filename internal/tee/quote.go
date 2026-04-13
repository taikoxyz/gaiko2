package tee

// Quote is the attestation payload returned by a TEE provider.
type Quote interface {
	Bytes() []byte
}

// StaticQuote is a raw quote container useful for tests and mock providers.
type StaticQuote []byte

func (q StaticQuote) Bytes() []byte {
	return q
}
