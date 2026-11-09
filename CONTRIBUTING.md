# Contributing

Thanks for helping. Bug reports and pull requests are welcome.

Before you open a pull request:

1. Run the checks CI runs:

   ```sh
   gofmt -l .           # should print nothing
   go vet ./...
   go test -race ./...
   golangci-lint run    # v2, see .golangci.yml
   ```

2. Add a test for the change. Tests use `httptest` servers; they must not call
   the real API or need an API key.
3. Keep the module free of third-party dependencies.
4. Give every exported identifier a doc comment.
5. Add a line under "Unreleased" in CHANGELOG.md.

Checkslip looks up a transfer with the sending bank and reports what the bank
returned; the customer decides whether to accept the payment. Please describe
lookups that way in code and docs.

For a security problem, follow SECURITY.md instead of opening an issue.
