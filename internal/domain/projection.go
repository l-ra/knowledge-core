package domain

type OutboxEvent struct {
	ID            string
	ChangeSetID   string
	EventType     string
	AggregateType string
	AggregateID   string
	Payload       map[string]any
	CreatedAt     string
	PublishedAt   *string
}

type SearchHit struct {
	ObjectType  string `json:"objectType"`
	PublicID    string `json:"publicId"`
	SubjectQID  string `json:"subjectQid,omitempty"`
	PropertyPID string `json:"propertyPid,omitempty"`
	SearchText  string `json:"searchText"`
}
