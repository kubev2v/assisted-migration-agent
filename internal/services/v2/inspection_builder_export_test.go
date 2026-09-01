package v2

// Exported for white-box regression tests in the external v2_test package.
// They assert every WorkUnit's Status() is side-effect-free: only Work() may
// persist "running" to the store. The pipeline calls Status() for each unit
// BEFORE dispatching that unit's Work, and the single v2v worker is released
// between units, so a Status() that wrote to the DB would flip a still-queued
// VM (or a parked VM sitting between units) to "running" before the worker
// actually picks it up.
var (
	DefaultV2VInspectionBuilderFactory      = defaultV2VInspectionBuilderFactory
	DefaultStandardInspectionBuilderFactory = defaultStandardInspectionBuilderFactory
)
