# Security policy

## Reporting a vulnerability

Email security@checkslip.com with a description of the issue, the SDK version
and, if you can, a way to reproduce it. Please do not open a public GitHub issue
for a security problem.

We will confirm that we received your report and keep you updated while we
work on a fix.

If the problem is in the Checkslip API rather than this SDK, send it to the
same address.

## Supported versions

Security fixes go into the latest release. Upgrade with:

```sh
go get github.com/Checkslip/sdk-go@latest
```

## Handling secrets

Never commit an API key (`csk_...`) or a webhook secret (`whsec_...`). If one
leaks, revoke the API key or rotate the webhook secret in the Checkslip
web app right away.
