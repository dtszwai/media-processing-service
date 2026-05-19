package generation

import (
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// stageWorkClass maps the persisted CurrentStage to the work_class label the
// dashboards filter on. It mirrors transition routing so metric labels and
// queue classes stay in the same closed set.
func stageWorkClass(job *generation.Job) generation.ResourceClass {
	switch job.CurrentStage {
	case generation.StageInputModeration, generation.StageOutputModeration,
		generation.StageCostReserve, generation.StagePromptPrepare, generation.StagePublish:
		return generation.ResourceFast
	case generation.StageProviderSubmit:
		return generation.ResourceProvider
	case generation.StageProviderWait:
		return generation.ResourcePoll
	case generation.StageDisclosurePostprocess:
		return resourceClassForPostprocess(job)
	default:
		return generation.ResourceFast
	}
}

func resourceClassForPostprocess(job *generation.Job) generation.ResourceClass {
	switch job.OutputType {
	case generation.OutputImage:
		return generation.ResourceImageProcess
	default:
		return generation.ResourceFast
	}
}
