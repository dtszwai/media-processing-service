package generation

type StageOutcome string

const (
	OutcomeModerationPassed       StageOutcome = "MODERATION_PASSED"
	OutcomeBudgetReserved         StageOutcome = "BUDGET_RESERVED"
	OutcomePromptPrepared         StageOutcome = "PROMPT_PREPARED"
	OutcomeProviderSubmittedAsync StageOutcome = "PROVIDER_SUBMITTED_ASYNC"
	OutcomeArtifactStaged         StageOutcome = "ARTIFACT_STAGED"
	OutcomePollPending            StageOutcome = "POLL_PENDING"
	OutcomeProviderJobFailed      StageOutcome = "PROVIDER_JOB_FAILED"
	OutcomeDisclosureComplete     StageOutcome = "DISCLOSURE_COMPLETE"
	OutcomePublished              StageOutcome = "PUBLISHED"
	OutcomeTransientRetry         StageOutcome = "TRANSIENT_RETRY"
)
