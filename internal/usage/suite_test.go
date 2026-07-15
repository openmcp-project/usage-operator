package usage_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestComponentUtils(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Usage Functions Test Suite")
}
