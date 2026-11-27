package tui

func (m *Model) markOperationStateUnknown() {
	m.snapshotStale = true
	message := m.tr("notification.state_unknown")
	m.snapshot.ServiceError, m.snapshot.ServiceDiagnostic = message, nil
	m.snapshot.Service.State = "unknown"
	m.snapshot.Groups = nil
	m.snapshot.APIError, m.snapshot.APIDiagnostic = message, nil
	m.snapshot.Mode.Actual = ""
	m.snapshot.RunningVersion = ""
}
