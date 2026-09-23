import AppKit
import Combine
import ServiceManagement

@MainActor final class LoginItem: ObservableObject {
    @Published private(set) var enabled = false
    @Published private(set) var needsApproval = false
    @Published private(set) var updating = false
    @Published private(set) var error: String?

    func refresh() {
        let status = SMAppService.mainApp.status
        enabled = status == .enabled || status == .requiresApproval
        needsApproval = status == .requiresApproval
    }

    func setEnabled(_ value: Bool) {
        guard !updating else { return }
        updating = true
        error = nil
        Task {
            do {
                if value {
                    try SMAppService.mainApp.register()
                } else {
                    try await SMAppService.mainApp.unregister()
                }
            } catch {
                self.error = "无法更新登录启动：\(error.localizedDescription)"
            }
            refresh()
            updating = false
        }
    }

    func openSystemSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }
}
