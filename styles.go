package main

import "github.com/charmbracelet/lipgloss"

var (
    titleStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#FF6E9C"))

    tabTasksActiveStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#1a1a1a")).
        Background(lipgloss.Color("#A8FF78")).
        Padding(0, 1)

    tabProjectsActiveStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#1a1a1a")).
        Background(lipgloss.Color("#FF9E64")).
        Padding(0, 1)

    tabTagsActiveStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#1a1a1a")).
        Background(lipgloss.Color("#d480f0")).
        Padding(0, 1)

    tabLearningsActiveStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#1a1a1a")).
        Background(lipgloss.Color("#FFE66D")).
        Padding(0, 1)

    tabStatsActiveStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#1a1a1a")).
        Background(lipgloss.Color("#78D4FF")).
        Padding(0, 1)

    tabInactiveStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#ffffff")).
        Background(lipgloss.Color("#333333")).
        Padding(0, 1)

    selectedStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#A8FF78")).
        Bold(true)

    normalStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FFFFFF"))

    overdueStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF0000")).
        Bold(true)

    depOverdueStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF9E64")).
        Bold(true)

    helpStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#888888"))

    detailTitleStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#FFFFFF"))

    detailLabelStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#FF6E9C"))

    detailValueStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FFFFFF"))

    detailSelectedStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#A8FF78")).
        Bold(true)

    inputStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("#FF6E9C")).
        Padding(0, 1)

    confirmStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF0000")).
        Bold(true)

    searchStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("#A8FF78")).
        Padding(0, 1)

    dimStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#555555"))

    listPanelStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("#FFFFFF")).
        Padding(0, 1).
        MarginLeft(2)

    detailPanelStyle = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("#FFFFFF")).
        Padding(0, 1).
        MarginLeft(2)


    ganttTodayStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF9E64")).
        Bold(true)

    ganttDoneStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#555555"))

    checkDoneStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#A8FF78")).
        Bold(true)

    headerStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#FF6E9C"))

    tagStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#d480f0")).
        Bold(true)

    tagSelectedStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#e8a0ff")).
        Bold(true)

    overdueCountStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF0000")).
        Bold(true)

    activeCountStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#A8FF78")).
        Bold(true)

    doneCountStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#555555"))

    pageIndicatorStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF9E64")).
        Bold(true)


    learningStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FFE66D")).
        Bold(true)

    learningSelectedStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FFF5A0")).
        Bold(true)



    statsHeaderStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#78D4FF")).
        Bold(true)
)

var ganttGradient = []lipgloss.Style{
    lipgloss.NewStyle().Foreground(lipgloss.Color("#2a5a14")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#2e6a18")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#327a1c")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#3a8c20")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#3a9c22")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#42aa28")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#4ebc30")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#5ccc36")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#6cd642")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#7adf52")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#88e860")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#98f06c")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#a8f878")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#a8ff78")),
}

var ganttOverdueGradient = []lipgloss.Style{
    lipgloss.NewStyle().Foreground(lipgloss.Color("#7a0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#8a0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#9b0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#aa0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#bc0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#ce0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#d40000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#da0000")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#de1111")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#e42222")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#e43333")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#ec4444")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#f45555")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#f86666")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8888")),
}

var tagProgressGradient = []lipgloss.Style{
    lipgloss.NewStyle().Foreground(lipgloss.Color("#1a0a2e")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#2d1b4e")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#3d2060")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#5a2d8a")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#7a3aaa")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#9b4cc8")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#b865e0")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#d480f0")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#e8a0ff")),
}

var statsGradient = []lipgloss.Style{
    lipgloss.NewStyle().Foreground(lipgloss.Color("#1a3a5c")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#2a5a8c")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#3a7aac")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#4a9acc")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#5abadc")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#6ad4ec")),
    lipgloss.NewStyle().Foreground(lipgloss.Color("#78d4ff")),
}
