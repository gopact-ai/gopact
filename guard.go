package gopact

// GuardRejection is the shared guard failure fact.
type GuardRejection struct {
	ID         string
	GuardName  string
	Phase      string
	Reason     string
	Message    string
	SubjectRef string
	RetryHint  *RetryHint
}

func (r GuardRejection) Error() string {
	if r.Reason == "" {
		return "gopact: guard rejected"
	}
	return "gopact: guard rejected: " + r.Reason
}
