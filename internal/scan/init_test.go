package scan

import "github.com/qiankunli/case-code-review/internal/session"

// Redirect session writes to test-sessions/ so this package's tests don't
// pollute the real sessions store.
func init() { session.UseTestSessions() }
