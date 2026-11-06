package telemetry

import (
	"context"
	"io"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Info 提供本次命令的资源关联信息，不收集环境或参数。
type Info struct {
	Service string
	Version string
	RunID   string
}

// Session 持有私有 provider；writer 由 diagnostics 会话持有。
type Session struct {
	Tracer        trace.Tracer
	provider      *sdktrace.TracerProvider
	config        Config
	warn          func(error)
	once          sync.Once
	closeErr      error
	cancelExports context.CancelFunc
	stopWarnings  func()
}

// Start 仅按显式配置创建 SDK；失败告警后返回可用的空 tracer。
func Start(ctx context.Context, config Config, info Info, writer io.Writer, warn func(error)) *Session {
	var warning sync.Once
	var gate sync.Mutex
	active := true
	report := func(err error) {
		if err == nil {
			return
		}
		gate.Lock()
		defer gate.Unlock()
		if active && warn != nil {
			warning.Do(func() {
				warn(err)
			})
		}
	}
	s := &Session{
		Tracer: noop.NewTracerProvider().Tracer(info.Service),
		config: config,
		warn:   report,
		stopWarnings: func() {
			gate.Lock()
			active = false
			gate.Unlock()
		},
	}
	if config.Exporter == None {
		return s
	}
	if err := validateSDKEnvironment(); err != nil {
		report(err)
		return s
	}
	var exporter sdktrace.SpanExporter
	var err error
	switch config.Exporter {
	case File:
		if writer == nil {
			report(writerunavailable.New())
			return s
		}
		exporter, err = stdouttrace.New(stdouttrace.WithWriter(writer))
	default:
		report(unknownexporter.New())
		return s
	}
	if err != nil {
		report(err)
		return s
	}
	life, cancelExports := context.WithCancel(context.WithoutCancel(ctx))
	s.cancelExports = cancelExports
	configuredResource := resource.NewSchemaless(
		attribute.String("service.name", info.Service),
		attribute.String("service.version", info.Version),
		attribute.String("service.instance.id", info.RunID),
	)
	processor := sdktrace.NewBatchSpanProcessor(guardedExporter{
		SpanExporter: exporter,
		warn:         report,
		life:         life,
		resource:     configuredResource,
	},
		sdktrace.WithMaxQueueSize(config.QueueSize), sdktrace.WithMaxExportBatchSize(config.BatchSize),
		sdktrace.WithBatchTimeout(config.BatchInterval), sdktrace.WithExportTimeout(config.ExportTimeout))
	s.provider = sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(processor),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{
			AttributeValueLengthLimit:   sdktrace.DefaultAttributeValueLengthLimit,
			AttributeValueDepthLimit:    sdktrace.DefaultAttributeValueDepthLimit,
			AttributeCountLimit:         sdktrace.DefaultAttributeCountLimit,
			EventCountLimit:             sdktrace.DefaultEventCountLimit,
			LinkCountLimit:              sdktrace.DefaultLinkCountLimit,
			AttributePerEventCountLimit: sdktrace.DefaultAttributePerEventCountLimit,
			AttributePerLinkCountLimit:  sdktrace.DefaultAttributePerLinkCountLimit,
		}),
		sdktrace.WithResource(configuredResource))
	s.Tracer = s.provider.Tracer(info.Service)
	return s
}

// Close 使用独立的有界清理 Context，业务取消不会阻止已结束 span 的导出。
func (s *Session) Close(ctx context.Context) error {
	s.once.Do(func() {
		if s.provider == nil {
			s.stopWarnings()
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.ShutdownTimeout)
		defer cancel()
		s.closeErr = s.provider.Shutdown(cleanupCtx)
		s.warn(s.closeErr)
		s.cancelExports()
		s.stopWarnings()
	})
	return s.closeErr
}

//nolint:containedctx // SDK 固定 ExportSpans 接口无法携带 Session 生命周期；此桥接用于中断关闭后的导出。
type guardedExporter struct {
	sdktrace.SpanExporter
	warn     func(error)
	life     context.Context
	resource *resource.Resource
}

func (e guardedExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	ctx, cancel := context.WithCancel(ctx)
	//nolint:contextcheck // 会话存活 Context 只终止后台导出，实际请求仍继承 SDK 传入的 ctx。
	stop := context.AfterFunc(e.life, cancel)
	defer stop()
	defer cancel()
	// 已处理的出口错误不再交给 SDK 的全局错误处理器，避免重复或未本地化的终端输出。
	// SDK 的 WithResource 会合并 OTEL_RESOURCE_ATTRIBUTES；出口仅保留入口明确提供的资源字段。
	filtered := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		filtered[i] = configuredSpan{ReadOnlySpan: span, resource: e.resource}
	}
	e.warn(e.SpanExporter.ExportSpans(ctx, filtered))
	return nil
}

func (e guardedExporter) Shutdown(ctx context.Context) error {
	e.warn(e.SpanExporter.Shutdown(ctx))
	return nil
}

type configuredSpan struct {
	sdktrace.ReadOnlySpan
	resource *resource.Resource
}

func (s configuredSpan) Resource() *resource.Resource {
	return s.resource
}
