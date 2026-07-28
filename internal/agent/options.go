package agent

// Options configures model selection and the future tool-turn budget.
type Options struct {
	Model    string
	MaxTurns int
}
