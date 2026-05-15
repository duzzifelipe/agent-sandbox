module github.com/duck-labs/agentsdx-cli

go 1.26.3

require (
	github.com/AlecAivazis/survey/v2 v2.3.7
	github.com/duck-labs/agentsdx-shared v0.0.0
	github.com/spf13/cobra v1.9.1
)

replace github.com/duck-labs/agentsdx-shared => ../shared
