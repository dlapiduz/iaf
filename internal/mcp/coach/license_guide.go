package coach

import (
	"context"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterLicenseGuide registers the license-guide prompt that explains SPDX
// identifiers, lists approved and prohibited licenses, and instructs agents to
// verify a dependency's license before adding it.
func RegisterLicenseGuide(server *gomcp.Server, _ *Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "license-guide",
		Description: "Open-source license policy for enterprise IAF applications. Explains SPDX identifiers, lists approved and prohibited licenses, and provides a check-before-add workflow.",
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		return &gomcp.GetPromptResult{
			Description: "Open-source license policy guide for IAF applications.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: licenseGuideText},
				},
			},
		}, nil
	})
}

const licenseGuideText = `# IAF License Policy Guide

## What Is an SPDX Identifier?

SPDX (Software Package Data Exchange) is the industry-standard way to identify open-source licenses.
Each license has a unique SPDX identifier (e.g. ` + "`MIT`" + `, ` + "`Apache-2.0`" + `, ` + "`GPL-3.0-only`" + `).
Always use SPDX IDs when referring to licenses — they are unambiguous and machine-readable.

Look up a dependency's license at:
- https://spdx.org/licenses/ — official SPDX license list
- Package registry pages (npmjs.com, pkg.go.dev, pypi.org, rubygems.org, mvnrepository.com)
- The ` + "`LICENSE`" + ` or ` + "`LICENSE.md`" + ` file in the project's repository

## Approved Licenses

The following SPDX licenses are pre-approved for use in enterprise IAF applications:

| SPDX ID | Name |
|---------|------|
| ` + "`MIT`" + ` | MIT License |
| ` + "`Apache-2.0`" + ` | Apache License 2.0 |
| ` + "`BSD-2-Clause`" + ` | BSD 2-Clause "Simplified" License |
| ` + "`BSD-3-Clause`" + ` | BSD 3-Clause "New" or "Revised" License |
| ` + "`ISC`" + ` | ISC License |

These licenses are permissive — they allow use, modification, and distribution in proprietary software
without requiring you to open-source your own code.

## Prohibited Licenses

The following SPDX licenses are **prohibited** in enterprise IAF applications:

| SPDX ID | Name | Why |
|---------|------|-----|
| ` + "`GPL-3.0-only`" + ` | GNU GPL v3 (exact) | Copyleft — requires distributing source of the combined work |
| ` + "`GPL-3.0-or-later`" + ` | GNU GPL v3 or later | Same as above |
| ` + "`AGPL-3.0-only`" + ` | GNU AGPL v3 (exact) | Network copyleft — triggers on server-side use |
| ` + "`AGPL-3.0-or-later`" + ` | GNU AGPL v3 or later | Same as above |

**Note on LGPL:** LGPL is permitted only when the library is dynamically linked. Do not bundle LGPL code
statically without consulting your legal team.

**Note on source-available licenses:** SSPL and BUSL licenses are NOT approved — despite open source
appearance, they restrict commercial use and are not OSI-approved.

## Check-Before-Add Workflow

**Before adding any new dependency:**

1. **Find the SPDX ID** — check the package registry or LICENSE file.
2. **Check the approved list** — if it is MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, or ISC, proceed.
3. **Check the prohibited list** — if it matches GPL-3.0-only/or-later or AGPL-3.0-only/or-later, **do not add it**.
4. **Unknown or unlicensed?** — Do **not** add the dependency. Flag it in your PR and wait for operator approval.
5. **Ambiguous?** (e.g. dual-licensed, custom license, ` + "`LicenseRef-*`" + ` SPDX ID) — Do **not** add it without approval.

## Unknown or Ambiguous License Action

If you cannot determine a clear, approved SPDX license:
- Do not add the dependency.
- Leave a comment in your code or PR: ` + "`# TODO: license unclear for <package> — needs operator approval`" + `
- Suggest an alternative dependency with a known-approved license if one exists.

## Machine-Readable Policy

Read ` + "`iaf://org/license-policy`" + ` for the full machine-readable policy JSON, which includes
the operator-configured approved list, prohibited list, and any additional notes.

## Important Caveat

IAF does **not** automatically scan or block builds based on license. This policy is advisory.
License compliance is the agent's responsibility — follow the check-before-add workflow above.
`
