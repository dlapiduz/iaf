# Architecture: Coach — Open Source License Policy (Issue #60)

## Approach

Add a `LicensePolicy` field to `OrgStandards` and expose it as a separate
`iaf://org/license-policy` MCP resource. A new `license-guide` prompt explains SPDX
identifiers and tells agents what to check before adding a dependency. The resource uses
the existing file-override pattern (`COACH_LICENSE_POLICY_FILE` env var + embedded
default JSON). No agent can modify the policy; it is read-only and operator-configured.

Depends on Issue #56 (coach server) for the package location: resource and prompt live
in `internal/mcp/coach/resources/` and `internal/mcp/coach/prompts/` respectively.

## Changes Required

### New files

- `internal/mcp/coach/resources/license_policy.go` — `RegisterLicensePolicy` function
- `internal/mcp/coach/resources/defaults/license-policy.json` — embedded default
- `internal/mcp/coach/prompts/license_guide.go` — `RegisterLicenseGuide` function

### Modified files

- `internal/orgstandards/loader.go` — add `LicensePolicy` struct and field to
  `OrgStandards`; update `platformDefaults()` with default values
- `internal/mcp/coach/deps.go` — add `LicensePolicyFile string` field
- `internal/mcp/coach/server.go` — call `resources.RegisterLicensePolicy` and
  `prompts.RegisterLicenseGuide`
- `internal/mcp/coach/prompts/coding_guide.go` — add cross-reference to `license-guide`
- `cmd/coachserver/main.go` — read `COACH_LICENSE_POLICY_FILE` env var
- `internal/mcp/coach/prompts/prompts_test.go` — update `TestListPrompts` count
- `internal/mcp/coach/resources/resources_test.go` — update `TestListResources` count

## Data / API Changes

### `LicensePolicy` type

```go
// internal/orgstandards/loader.go

// LicensePolicy defines which open source licenses are permitted in this org.
type LicensePolicy struct {
    // ApprovedSpdxIds lists SPDX identifiers that are approved for use.
    // See https://spdx.org/licenses/ for the full list.
    ApprovedSpdxIds []string `json:"approvedSpdxIds" yaml:"approvedSpdxIds"`

    // ProhibitedSpdxIds lists SPDX identifiers that must not be used.
    ProhibitedSpdxIds []string `json:"prohibitedSpdxIds" yaml:"prohibitedSpdxIds"`

    // Notes contains additional guidance that doesn't fit the approved/prohibited model.
    Notes []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// In OrgStandards:
type OrgStandards struct {
    // ... existing fields ...
    LicensePolicy *LicensePolicy `json:"licensePolicy,omitempty" yaml:"licensePolicy,omitempty"`
}
```

### Platform defaults

```go
func platformDefaults() *OrgStandards {
    return &OrgStandards{
        // ... existing defaults ...
        LicensePolicy: &LicensePolicy{
            ApprovedSpdxIds: []string{
                "MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC",
            },
            ProhibitedSpdxIds: []string{
                "GPL-3.0-only", "GPL-3.0-or-later",
                "AGPL-3.0-only", "AGPL-3.0-or-later",
            },
            Notes: []string{
                "LGPL-2.1-only and LGPL-3.0-only are permitted for dynamic linking only — check with your legal team before using LGPL dependencies that require static linking.",
                "When a dependency's license is unknown or unlisted here, do not add it without operator approval.",
            },
        },
    }
}
```

### Embedded default `defaults/license-policy.json`

```json
{
  "approvedSpdxIds": ["MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC"],
  "prohibitedSpdxIds": ["GPL-3.0-only", "GPL-3.0-or-later", "AGPL-3.0-only", "AGPL-3.0-or-later"],
  "notes": [
    "LGPL is permitted for dynamic linking only.",
    "When a license is unknown, do not add the dependency without operator approval."
  ]
}
```

### `RegisterLicensePolicy` resource

```go
// internal/mcp/coach/resources/license_policy.go

//go:embed defaults/license-policy.json
var defaultLicensePolicy []byte

func RegisterLicensePolicy(server *gomcp.Server, deps *coach.Dependencies) {
    server.AddResource(&gomcp.Resource{
        URI:         "iaf://org/license-policy",
        Name:        "org-license-policy",
        Description: "Organisation open-source license policy — approved SPDX identifiers, prohibited licenses, and guidance for unknown or ambiguous licenses.",
        MIMEType:    "application/json",
    }, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
        content, err := loadStandards(deps.LicensePolicyFile, defaultLicensePolicy)
        if err != nil {
            return nil, fmt.Errorf("loading license policy: %w", err)
        }
        return &gomcp.ReadResourceResult{
            Contents: []*gomcp.ResourceContents{
                {URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
            },
        }, nil
    })
}
```

### `license-guide` prompt content outline

```
# License Policy Guide

## What is an SPDX identifier?
SPDX (Software Package Data Exchange) identifiers are standardised short names for
open-source licenses. Examples: MIT, Apache-2.0, GPL-3.0-only. Always use the exact
SPDX identifier when checking or documenting a dependency's license.

## Before adding a dependency
1. Find the dependency's license (check the source repo, package registry, or NOTICE file).
2. Look up the SPDX identifier at https://spdx.org/licenses/
3. Check it against the approved/prohibited lists in iaf://org/license-policy.
4. If the license is APPROVED: proceed.
5. If the license is PROHIBITED: do not add the dependency. Find an alternative.
6. If the license is UNKNOWN or NOT LISTED: do not add it without operator approval.
   Submit a request to your platform team with the dependency name and license.

## Current policy
Read iaf://org/license-policy for the machine-readable list of approved and
prohibited SPDX identifiers configured for this organisation.

## Common pitfalls
- MIT and ISC look similar but are distinct SPDX IDs — check both.
- GPL-3.0-only and GPL-3.0-or-later are both prohibited by default.
- Transitive dependencies also carry license obligations — check your full dependency tree.
- Dual-licensed packages (e.g. MIT OR Apache-2.0): any approved SPDX ID in the
  OR list makes the package acceptable.

## Enforcement note
IAF does not currently block builds based on license. This policy is advisory.
Automated license scanning at build time is planned as a future work item.
```

### Cross-reference in `coding-guide`

Add to the end of the coding guide:

```
## License Policy
Before adding any dependency, check its license against the organisation policy.
Read the `license-guide` prompt and `iaf://org/license-policy` for approved and
prohibited SPDX identifiers.
```

## Multi-tenancy & Shared Resource Impact

Stateless, read-only. No K8s access. One policy per coach deployment (org-wide).

## Security Considerations

- **Read-only**: agents can only read the policy, never modify it. No write path.
- **File path**: `COACH_LICENSE_POLICY_FILE` is validated by `loadStandards` (rejects
  `..` sequences, caps at 1 MB).
- **No build enforcement**: the policy is advisory. Agents are informed but not blocked.
  This is an explicit design choice — automated enforcement is deferred.
- **SPDX IDs as strings**: SPDX identifiers are display-only strings. No code evaluates
  or executes them. No injection risk.

## Resource & Performance Impact

Negligible. Small embedded JSON (~200 bytes), loaded once at startup.

## Migration / Compatibility

- Additive: `LicensePolicy` is `omitempty` in `OrgStandards`, so existing configs
  without this field continue to work and receive the platform default.
- `iaf://org/license-policy` is a new resource URI — no existing agent code references it.
- Test assertion counts for `TestListPrompts` and `TestListResources` must be updated.

## Open Questions for Developer

- Should `LicensePolicy` live in `OrgStandards` (as above) or be a completely separate
  config struct? Recommendation: add to `OrgStandards` for simplicity; the resource uses
  its own file-override path regardless, so the coupling is loose.
- Should the `license-guide` prompt accept an optional `dependency` argument to filter
  the output? Recommendation: no, keep it simple; the agent can read the full policy.
