package derive

import (
	"context"
	"errors"
	"io"

	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/ethereum-optimism/optimism/op-service/eth"
)

type DeriverIdleEvent struct{}

func (d DeriverIdleEvent) String() string {
	return "derivation-idle"
}

type DeriverMoreEvent struct{}

func (d DeriverMoreEvent) String() string {
	return "deriver-more"
}

type ConfirmPipelineResetEvent struct{}

func (d ConfirmPipelineResetEvent) String() string {
	return "confirm-pipeline-reset"
}

// DerivedAttributesEvent is emitted when new attributes are available to apply to the engine.
type DerivedAttributesEvent struct {
	Attributes *AttributesWithParent
}

func (ev DerivedAttributesEvent) String() string {
	return "derived-attributes"
}

type PipelineStepEvent struct {
	PendingSafe eth.L2BlockRef
}

func (ev PipelineStepEvent) String() string {
	return "pipeline-step"
}

type PipelineDeriver struct {
	pipeline *DerivationPipeline

	ctx context.Context

	emitter rollup.EventEmitter
}

func NewPipelineDeriver(ctx context.Context, pipeline *DerivationPipeline, emitter rollup.EventEmitter) *PipelineDeriver {
	return &PipelineDeriver{
		pipeline: pipeline,
		ctx:      ctx,
		emitter:  emitter,
	}
}

func (d *PipelineDeriver) OnEvent(ev rollup.Event) {
	switch x := ev.(type) {
	case rollup.ResetEvent:
		d.pipeline.Reset()
	case PipelineStepEvent:
		d.pipeline.log.Trace("Derivation pipeline step", "onto_origin", d.pipeline.Origin())
		attrib, err := d.pipeline.Step(d.ctx, x.PendingSafe)
		if err == io.EOF {
			d.pipeline.log.Debug("Derivation process went idle", "progress", d.pipeline.Origin(), "err", err)
			d.emitter.Emit(DeriverIdleEvent{})
		} else if err != nil && errors.Is(err, EngineELSyncing) {
			d.pipeline.log.Debug("Derivation process went idle because the engine is syncing", "progress", d.pipeline.Origin(), "err", err)
			d.emitter.Emit(DeriverIdleEvent{})
		} else if err != nil && errors.Is(err, ErrReset) {
			d.emitter.Emit(rollup.ResetEvent{Err: err})
		} else if err != nil && errors.Is(err, ErrTemporary) {
			d.emitter.Emit(rollup.EngineTemporaryErrorEvent{Err: err})
		} else if err != nil && errors.Is(err, ErrCritical) {
			d.emitter.Emit(rollup.CriticalErrorEvent{Err: err})
		} else if err != nil && errors.Is(err, NotEnoughData) {
			// don't do a backoff for this error
			d.emitter.Emit(DeriverMoreEvent{})
		} else if err != nil {
			d.pipeline.log.Error("Derivation process error", "err", err)
			d.emitter.Emit(rollup.EngineTemporaryErrorEvent{Err: err})
		} else {
			if attrib != nil {
				d.emitter.Emit(DerivedAttributesEvent{Attributes: attrib})
			} else {
				d.emitter.Emit(DeriverMoreEvent{}) // continue with the next step if we can
			}
		}
	case ConfirmPipelineResetEvent:
		d.pipeline.ConfirmEngineReset()
	}
}
