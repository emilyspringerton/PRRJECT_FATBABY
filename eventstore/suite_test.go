package eventstore_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEventStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EventStore Suite")
}
