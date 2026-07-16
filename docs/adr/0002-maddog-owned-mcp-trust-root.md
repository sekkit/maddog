# Maddog owns the MCP trust root

Maddog trusts only a Maddog-signed capability catalog together with explicit
local capability receipts. A Reasonix catalog may be inspected as an upstream
source, but its entries remain untrusted until Maddog reviews and re-signs them;
otherwise Reasonix's release authority would become a remote-code-execution
trust root for Maddog.

Catalog keys rotate only through signed Maddog releases. Catalog rollback,
mutable launcher references, package drift, live revocation, and receipt
identity or capability drift fail closed before process or network startup.
Custom user trust remains session- or workspace-scoped and never becomes
official global authority.
