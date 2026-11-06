package logging

import (
	"github.com/zhYou/my/internal/base/fault"
)

var writerunavailable = fault.Definition{Code: "logging.writer_unavailable", Kind: fault.Runtime}
