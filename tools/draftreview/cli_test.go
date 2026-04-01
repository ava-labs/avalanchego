package draftreview

import "testing"

func TestParseDeleteCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"delete", "--pr", "5168"})
	if err != nil {
		t.Fatalf("parseCommand returned error: %v", err)
	}

	deleteCommand, ok := command.(deleteCommand)
	if !ok {
		t.Fatalf("unexpected command type %T", command)
	}
	if deleteCommand.Repo != defaultRepo {
		t.Fatalf("unexpected repo %q", deleteCommand.Repo)
	}
	if deleteCommand.PRNumber != 5168 {
		t.Fatalf("unexpected pr number %d", deleteCommand.PRNumber)
	}
}
