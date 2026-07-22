package main

import "testing"

func TestBoardTabUsesDistinctThemeColor(t *testing.T) {
	t.Cleanup(func() { applyTheme(themes[0]) })

	for _, theme := range themes {
		t.Run(theme.name, func(t *testing.T) {
			applyTheme(theme)

			if got := tabBoardActiveStyle.GetBackground(); got != theme.yellow {
				t.Errorf("active Board background = %v, want theme yellow %v", got, theme.yellow)
			}
			if got := tabBoardInactiveStyle.GetForeground(); got != theme.yellow {
				t.Errorf("inactive Board foreground = %v, want theme yellow %v", got, theme.yellow)
			}
			if tabBoardActiveStyle.GetBackground() == tabTagsActiveStyle.GetBackground() {
				t.Error("active Board and Tags tabs must use distinct colors")
			}
			if tabBoardInactiveStyle.GetForeground() == tabTagsInactiveStyle.GetForeground() {
				t.Error("inactive Board and Tags tabs must use distinct colors")
			}
		})
	}
}
