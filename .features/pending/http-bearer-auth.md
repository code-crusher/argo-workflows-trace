Description: Bearer token authentication for HTTP artifacts
Authors: [Claude](https://github.com/anthropics)
Component: General
Issues: 15778

HTTP artifacts support a new `bearer` authentication method alongside `basicAuth`, `oauth2` and `clientCert`.
You supply the token from a secret, so it stays out of the Workflow spec:

`http:`
`  url: https://example.com/download`
`  auth:`
`    bearer:`
`      tokenSecret:`
`        name: my-secret`
`        key: token`

The driver sends the token as an `Authorization: Bearer <token>` header on both downloads and uploads.
A literal `Authorization` header set with `http.headers` is overridden by the bearer token.
