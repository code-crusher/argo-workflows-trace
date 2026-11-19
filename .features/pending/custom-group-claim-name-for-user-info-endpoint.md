Description: Read groups from `customGroupClaimName` in the user-info endpoint
Authors: [okzw999](https://github.com/okzw999)
Component: General
Issues: 15803

Argo Server now applies `sso.customGroupClaimName` to the response from the endpoint set in `sso.userInfoPath`.
Previously it always read groups from the `groups` claim of that response, so users of an OIDC provider that returns them under another claim name had no groups in Argo.
Set both options when your provider returns groups only from its user-info endpoint, under a claim name other than `groups`.
Leaving `customGroupClaimName` unset keeps reading the `groups` claim.
