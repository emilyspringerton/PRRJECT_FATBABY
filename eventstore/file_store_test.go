package eventstore_test

import (
	"github.com/example/prrject-fatbaby/eventstore"
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("FileStore", func() {
	eventStoreContract(func(rootDir string) (eventstore.EventStore, error) {
		return eventstore.NewFileStore(rootDir)
	})
})
