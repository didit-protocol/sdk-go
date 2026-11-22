// The spec sync classification fixtures (codegen/sync_classify_test.go):
// gzipped copies of past upstream documents. This go.mod keeps them out of
// the module users download (Go leaves out every directory with its own
// go.mod); the tests skip when they are missing.
module github.com/didit-protocol/sdk-go/codegen/testdata/spec-sync

go 1.22
