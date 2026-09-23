import Foundation

@main struct StoreTests {
    @MainActor static func wait(_ label: String, _ condition: @escaping () -> Bool) async throws {
        for _ in 0..<240 {
            if condition() { return }
            try await Task.sleep(nanoseconds: 50_000_000)
        }
        throw NSError(domain: "NativeTest", code: 1, userInfo: [NSLocalizedDescriptionKey: "Timed out: \(label)"])
    }
    static func emit(_ text: String) { print(text); fflush(stdout) }
    @MainActor static func main() async throws {
        let url = CommandLine.arguments[1]
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("EasyNotifyTests-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = Store(directory: directory, deliverSystemNotifications: false)
        defer { store.disconnect() }
        for invalid in ["ftp://localhost", "http://host/path", "http://user:pass@host", "http://host?key=x", "http://host#fragment"] {
            assert(Store.socketURL(invalid) == nil)
        }
        assert(!store.save(server: url, key: " "))
        assert(store.save(server: url, key: "wrong-key"))
        try await wait("authentication failure") { store.state == "鉴权失败" }
        assert(store.save(server: url, key: "native-test-key"))
        try await wait("connected") { store.connected }
        emit("SEND_FIRST")
        try await wait("first receive") { store.messages.count == 1 }
        assert(store.messages[0].description == "# Native\n\n**Markdown**")
        store.delete(store.messages[0].id)
        assert(store.messages.isEmpty && store.selected == nil)
        let saved = try JSONDecoder().decode([Notice].self, from: Data(contentsOf: directory.appendingPathComponent("messages.json")))
        assert(saved.isEmpty)
        emit("SEND_SECOND")
        try await wait("second receive") { store.messages.count == 1 }
        let reloaded = Store(directory: directory, deliverSystemNotifications: false)
        assert(reloaded.messages == store.messages)
        assert(reloaded.configuration.key == "native-test-key")
        reloaded.disconnect()
        func post(_ title: String) async throws {
            var request = URLRequest(url: URL(string: url + "/notify")!)
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: ["title": title, "description": "queued"])
            let (data, response) = try await URLSession.shared.data(for: request)
            assert((response as? HTTPURLResponse)?.statusCode == 200)
            let result = try JSONSerialization.jsonObject(with: data) as? [String: Any]
            assert(result?["accepted"] as? Bool == true)
        }
        store.disconnect()
        try await post("offline one")
        try await post("offline two")
        assert(store.messages.count == 1)
        store.connect()
        try await wait("offline backlog") { store.messages.count == 3 }
        let duplicate = store.messages[0]
        try store.receive(duplicate)
        assert(store.messages.count == 3)
        store.disconnect()
        let history = directory.appendingPathComponent("messages.json")
        let historyData = try Data(contentsOf: history)
        try FileManager.default.removeItem(at: history)
        try FileManager.default.createDirectory(at: history, withIntermediateDirectories: false)
        try await post("retry persistence")
        store.connect()
        try await wait("persistence failure") { store.error?.contains("无法保存消息") == true }
        assert(store.messages.count == 3)
        store.disconnect()
        try FileManager.default.removeItem(at: history)
        try historyData.write(to: history)
        store.connect()
        try await wait("replay after storage recovery") { store.messages.count == 4 }
        assert(store.error == nil)
        emit("STOP_RELAY")
        try await wait("disconnected") { !store.connected }
        emit("RESTART_RELAY")
        try await wait("reconnected") { store.connected }
        emit("SEND_THIRD")
        try await wait("receive after reconnect") { store.messages.count == 5 }
        let attributes = try FileManager.default.attributesOfItem(atPath: directory.appendingPathComponent("config.json").path)
        assert((attributes[.posixPermissions] as? NSNumber)?.intValue == 0o600)
        emit("PASS: native authentication, validation, receive, deletion, persistence, private config, offline backlog, deduplication, persistence failure replay, reconnect and receive after restart")
    }
}
