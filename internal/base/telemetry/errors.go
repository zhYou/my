package telemetry

import (
	"github.com/zhYou/my/internal/base/fault"
)

var (
	writerunavailable = fault.Definition{Code: "telemetry.writer_unavailable", Kind: fault.Runtime}
	unknownexporter   = fault.Definition{Code: "telemetry.unknown_exporter", Kind: fault.Runtime}
	sdkenvironment    = fault.Definition{Code: "telemetry.sdk_environment", Kind: fault.Runtime}
)
