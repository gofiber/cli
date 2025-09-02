# Migration Guidelines

- Migration functions must print a message only if they performed changes.
- Use `internal.ChangeFileContent`, which returns a boolean, and print the message only when it is `true`.
