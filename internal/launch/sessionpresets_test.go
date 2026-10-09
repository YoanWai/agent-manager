package launch

import "testing"

func TestWithInstructionsPreservesBytes(t *testing.T) {
	tests := []struct{ name, instructions, task, want string }{
		{"both empty", "", "", ""},
		{"no instructions", "", " \tTask\n", " \tTask\n"},
		{"no task", " \t指示\n", "", " \t指示\n"},
		{"both", " \t指示\n", "\n Task \t", " \t指示\n\n\n\n Task \t"},
		{"whitespace is literal", " ", "\t", " \n\n\t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithInstructions(tc.instructions, tc.task); got != tc.want {
				t.Fatalf("WithInstructions = %q, want %q", got, tc.want)
			}
		})
	}
}
