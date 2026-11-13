package profile

type Node struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Outbound map[string]any `json:"outbound"`
}

type NodeList struct {
	Nodes []Node `json:"nodes"`
}

type Subscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url,omitempty"`
	Format    string `json:"format,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Nodes     []Node `json:"nodes"`
}

type Subscriptions struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

type Selection struct {
	Source string `json:"source"`
	Node   string `json:"node"`
}

type Store struct {
	Dir string
	// In-memory source documents are used only for validating editor drafts.
	documents map[string][]byte
}
