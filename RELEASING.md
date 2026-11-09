# Releasing

Releases are driven by the commit messages on `main`. Nobody edits a version
number by hand.

## How it works

1. Every merge to `main` updates a release pull request, opened by
   release-please. It works out the next version from the Conventional Commits
   since the last release, writes `CHANGELOG.md` and bumps the `Version`
   constant in `client.go`.
2. Merging that pull request tags `vX.Y.Z` and publishes a GitHub Release.
3. The release triggers `publish.yml`, which waits for the checks on that exact
   commit, checks the tag against the `Version` constant, runs the tests again,
   warms the module proxy so the first `go get` is fast, and attaches the module
   zip to the release.

There is nothing to publish and no token anywhere: the Go module proxy and
pkg.go.dev read the tag straight from this repository.

`feat:` gives a minor bump, `fix:` a patch, and a `!` or a `BREAKING CHANGE:`
footer a major one. Before 1.0.0 a breaking change bumps the minor.

## Things to know

- Allow GitHub Actions to create pull requests in the repository settings, or
  release-please cannot open its release pull request.
- A published tag is immutable in the proxy cache. A bad release cannot be
  replaced: cut the next patch and add a `retract` directive to `go.mod`.
- Major version 2 and up needs the suffix in both the module path and the tag,
  `github.com/Checkslip/sdk-go/v2` and `v2.0.0`.
