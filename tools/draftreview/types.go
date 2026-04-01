package draftreview

const reviewStatePending = "PENDING"

type User struct {
	Login string `json:"login"`
}

type Review struct {
	ID      int64  `json:"id"`
	State   string `json:"state"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	User    User   `json:"user"`
}
