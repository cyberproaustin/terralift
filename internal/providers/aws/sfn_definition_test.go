package aws

import (
	"strings"
	"testing"
)

// TestReplaceSFNDefinitionParensInStrings locks the fix for span tracking that used
// to count parentheses: an ASL Comment/state string carrying an unbalanced paren
// (here a smiley ":)") would end the jsonencode span early, truncating the block.
// The string/comment-aware brace tracking must consume the whole jsonencode({...})
// and leave the following attribute intact.
func TestReplaceSFNDefinitionParensInStrings(t *testing.T) {
	block := []string{
		`resource "aws_sfn_state_machine" "sm" {`,
		`  name       = "x"`,
		`  definition = jsonencode({`,
		`    Comment = "ends with a smiley :)"`,
		`    StartAt = "P"`,
		`    States = {`,
		`      P = { Type = "Pass", End = true }`,
		`    }`,
		`  })`,
		`  role_arn = "arn:aws:iam::123:role/app"`,
		`}`,
	}
	raw := `{"Comment":"ends with a smiley :)","StartAt":"P","States":{"P":{"Type":"Pass","End":true}}}`

	out, replaced := replaceSFNDefinition(block, raw)
	if !replaced {
		t.Fatal("definition was not replaced")
	}
	joined := strings.Join(out, "\n")

	if !strings.Contains(joined, `definition = "{\"Comment\"`) {
		t.Errorf("definition not replaced with the escaped literal:\n%s", joined)
	}
	// The jsonencode wrapper and its HCL object lines must be fully consumed. (Note
	// "StartAt" still appears INSIDE the escaped JSON literal — assert on the HCL
	// attribute form `StartAt = ` and the `})` closer, which must be gone.)
	if strings.Contains(joined, "jsonencode") || strings.Contains(joined, "StartAt = ") || strings.Contains(joined, "})") {
		t.Errorf("jsonencode span was not fully consumed (early stop truncated it):\n%s", joined)
	}
	if !strings.Contains(joined, `role_arn = "arn:aws:iam::123:role/app"`) {
		t.Errorf("the attribute after the definition was swallowed:\n%s", joined)
	}
	if !strings.HasSuffix(strings.TrimRight(joined, "\n"), "}") {
		t.Errorf("block no longer ends with its closing brace:\n%s", joined)
	}
}
