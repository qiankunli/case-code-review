package agent

import "github.com/qiankunli/case-code-review/internal/session"

// Redirect session writes to test-sessions/ so agent tests (which construct
// Agents, and thus SessionHistory) don't pollute the real sessions store.
func init() { session.UseTestSessions() }
