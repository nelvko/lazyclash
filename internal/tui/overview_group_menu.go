package tui

const overviewRouteGroupTarget = "overview.routeGroup"

func (o dashboardOptions) overviewRuleGroupDropdown() dropdown {
	index, canChoose := o.overviewRouteGroup()
	if !canChoose {
		return dropdown{}
	}
	d := dropdown{Value: o.Snapshot.Groups[index].Name, CenterPopup: true, ShowCursor: true}
	for _, group := range o.Snapshot.Groups {
		if group.Name != "GLOBAL" {
			d.Items = append(d.Items, menuItem{ID: group.Name, Label: group.Name})
		}
	}
	return d
}

func (m *Model) applyOverviewGroupChoice(result menuResult) {
	if !result.Closed {
		return
	}
	m.hover = ""
	if !m.pageSelectionMenuAvailable("overview") {
		return
	}
	if result.Action == "" {
		m.revealOverviewGroupSelection()
		return
	}
	d := m.navigationOptions().overviewRuleGroupDropdown()
	for _, item := range d.Items {
		if item.ID == result.Action {
			m.overviewGroup = item.ID
			m.revealOverviewGroupSelection()
			return
		}
	}
}

func (m *Model) revealOverviewGroupSelection() {
	o := m.navigationOptions()
	for _, card := range overviewCardRegionsFromModel(*m) {
		if card.ID == "overview.card.dashboard.control.routingMode" {
			// The route ends above the bottom padding in the unpadded
			// card. Extra rows from a taller neighbor are irrelevant here.
			end := card.Y + len(overviewModeCard(o, card.W).lines) - 2
			m.revealBodyTarget(hitRegion{Y: end, H: 1})
			return
		}
	}
}

func (m *Model) reconcileOverviewGroupMenu() {
	d := m.navigationOptions().overviewRuleGroupDropdown()
	if len(d.Items) < 2 || !m.pageSelectionMenuAvailable("overview") {
		m.menu.dismiss()
		m.hover = ""
		if m.focus == overviewRouteGroupTarget {
			m.focus = "overview.card.dashboard.control.routingMode"
		}
		return
	}
	d.sync(&m.menu)
}
