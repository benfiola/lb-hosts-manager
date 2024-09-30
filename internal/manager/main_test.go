package manager

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ManagerTestSuite struct {
	suite.Suite
}

func TestManagerTestSuite(t *testing.T) {
	suite.Run(t, new(ManagerTestSuite))
}

func (s *ManagerTestSuite) TestEmpty() {

}
