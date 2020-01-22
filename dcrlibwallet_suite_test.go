package dcrlibwallet_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestDcrlibwallet(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dcrlibwallet Suite")
}
