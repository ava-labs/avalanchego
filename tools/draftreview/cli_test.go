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
	if deleteCommand.StateDir == "" {
		t.Fatalf("expected state dir")
	}
}

func TestParseGetCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"get", "--pr", "5168"})
	if err != nil {
		t.Fatalf("parseCommand returned error: %v", err)
	}

	getCommand, ok := command.(getCommand)
	if !ok {
		t.Fatalf("unexpected command type %T", command)
	}
	if getCommand.PRNumber != 5168 {
		t.Fatalf("unexpected pr number %d", getCommand.PRNumber)
	}
	if getCommand.StateDir == "" {
		t.Fatalf("expected state dir")
	}
}

func TestParseUpdateBodyCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"update-body", "--pr", "5168", "--body", "test"})
	if err != nil {
		t.Fatalf("parseCommand returned error: %v", err)
	}

	updateBodyCommand, ok := command.(updateBodyCommand)
	if !ok {
		t.Fatalf("unexpected command type %T", command)
	}
	if updateBodyCommand.PRNumber != 5168 {
		t.Fatalf("unexpected pr number %d", updateBodyCommand.PRNumber)
	}
	if updateBodyCommand.Body != "test" {
		t.Fatalf("unexpected body %q", updateBodyCommand.Body)
	}
	if updateBodyCommand.StateDir == "" {
		t.Fatalf("expected state dir")
	}
	if updateBodyCommand.Force {
		t.Fatalf("expected force to default to false")
	}
}

func TestParseUpdateBodyCommandForce(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"update-body", "--pr", "5168", "--body", "test", "--force"})
	if err != nil {
		t.Fatalf("parseCommand returned error: %v", err)
	}

	updateBodyCommand, ok := command.(updateBodyCommand)
	if !ok {
		t.Fatalf("unexpected command type %T", command)
	}
	if !updateBodyCommand.Force {
		t.Fatalf("expected force to be true")
	}
}
