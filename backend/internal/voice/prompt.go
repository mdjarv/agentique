package voice

import (
	"fmt"
	"strings"
)

// ReportingInstructions is appended to a drafted prompt when the operator has
// chosen to stay on the call.
//
// It is conditional on purpose. A run nobody is listening to should carry none
// of this: no instruction, no tool calls, no reporting overhead. That is the
// whole reason the handoff asks rather than assuming.
//
// toolName is the full MCP tool name so the worker can find it.
func ReportingInstructions(toolName string) string {
	var b strings.Builder
	b.WriteString("## Someone is listening\n\n")
	b.WriteString("A person is on a live voice call following this run. ")
	b.WriteString(fmt.Sprintf("Use `%s` to tell them something.\n\n", toolName))

	b.WriteString("**When to call it.** At decision points and surprises — the things ")
	b.WriteString("that would change what they'd ask you to do next. A test suite that was ")
	b.WriteString("already failing before you touched it. A file that isn't where the task ")
	b.WriteString("assumed. An approach you've abandoned for a different one.\n\n")

	b.WriteString("**When not to.** Progress is not news. Do not report opening a file, ")
	b.WriteString("running a command, or finishing a routine step. They can see all of that ")
	b.WriteString("on screen; the call is for what the screen won't tell them.\n\n")

	b.WriteString("**Budget.** Expect two or three calls in a ten-minute run, not twenty. ")
	b.WriteString("Reporting too often is worse than not reporting: it trains them to stop ")
	b.WriteString("listening. You do not need to report that you have finished — that is ")
	b.WriteString("delivered automatically.\n\n")

	b.WriteString("**It will be read aloud.** Write one plain spoken sentence. No markdown, ")
	b.WriteString("no bullet lists, no code, no file paths unless the path is the point. ")
	b.WriteString("Write to the person, not about them: \"the auth tests were already ")
	b.WriteString("failing on main\", not \"Note: pre-existing test failures detected.\"\n")

	return b.String()
}
