package telemetry

import (
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// validateSDKEnvironment 防止 SDK 在显式选项生效前将异常环境值写入全局 stderr。
// 仅检查 SDK 无法跳过的构造路径；本地出口不读取远程导出环境变量。
// 无法安全构造时只做本地化降级告警，不改写进程环境或全局 SDK handler。
func validateSDKEnvironment() error {
	invalid := func() error {
		return sdkenvironment.New()
	}
	for _, key := range []string{
		"OTEL_BSP_SCHEDULE_DELAY", "OTEL_BSP_EXPORT_TIMEOUT", "OTEL_BSP_MAX_QUEUE_SIZE", "OTEL_BSP_MAX_EXPORT_BATCH_SIZE",
		"OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT", "OTEL_ATTRIBUTE_COUNT_LIMIT", "OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT",
		"OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT", "OTEL_SPAN_EVENT_COUNT_LIMIT", "OTEL_EVENT_ATTRIBUTE_COUNT_LIMIT",
		"OTEL_SPAN_LINK_COUNT_LIMIT", "OTEL_LINK_ATTRIBUTE_COUNT_LIMIT",
	} {
		if raw := os.Getenv(key); raw != "" {
			if _, err := strconv.Atoi(raw); err != nil {
				return invalid()
			}
		}
	}
	sampler := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	switch sampler {
	case "", "always_on", "always_off", "parentbased_always_on", "parentbased_always_off":
	case "traceidratio", "parentbased_traceidratio":
		if arg := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); arg != "" {
			ratio, err := strconv.ParseFloat(arg, 64)
			if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
				return invalid()
			}
		}
	default:
		return invalid()
	}
	if attributes := strings.TrimSpace(os.Getenv("OTEL_RESOURCE_ATTRIBUTES")); attributes != "" {
		for pair := range strings.SplitSeq(attributes, ",") {
			_, value, ok := strings.Cut(pair, "=")
			if !ok {
				return invalid()
			}
			if _, err := url.PathUnescape(strings.TrimSpace(value)); err != nil {
				return invalid()
			}
		}
	}
	return nil
}
