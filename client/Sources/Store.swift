import Foundation
import Combine
import UserNotifications

struct Notice: Codable, Identifiable, Hashable {
    let id: String
    let title: String
    let description: String
    let createdAt: String
    var date: Date {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: createdAt) ?? Date.distantPast
    }
}
struct Configuration: Codable {
    var server = "http://127.0.0.1:8787"
    var key = ""
}

@MainActor final class Store: NSObject, ObservableObject, URLSessionWebSocketDelegate {
    @Published var messages: [Notice] = []
    @Published var selected: String?
    @Published var state = "未配置"
    @Published var connected = false
    @Published var error: String?
    @Published var configuration = Configuration()
    @Published var settingsVisible = false
    let directory: URL
    private let deliverSystemNotifications: Bool
    private var session: URLSession!
    private var socket: URLSessionWebSocketTask?
    private var connectionLoop: Task<Void, Never>?
    private var heartbeat: Task<Void, Never>?
    private var generation = UUID()

    init(directory: URL, deliverSystemNotifications: Bool = true) {
        self.directory = directory
        self.deliverSystemNotifications = deliverSystemNotifications
        super.init()
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            let configURL = directory.appendingPathComponent("config.json")
            if FileManager.default.fileExists(atPath: configURL.path) {
                configuration = try JSONDecoder().decode(Configuration.self, from: Data(contentsOf: configURL))
            }
            let messagesURL = directory.appendingPathComponent("messages.json")
            if FileManager.default.fileExists(atPath: messagesURL.path) {
                messages = try JSONDecoder().decode([Notice].self, from: Data(contentsOf: messagesURL))
            }
        } catch { self.error = "无法读取本地数据：\(error.localizedDescription)" }
        selected = messages.first?.id
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 15
        session = URLSession(configuration: config, delegate: self, delegateQueue: .main)
    }

    static func socketURL(_ text: String) -> URL? {
        guard var url = URLComponents(string: text.trimmingCharacters(in: .whitespacesAndNewlines)),
              ["http", "https"].contains(url.scheme), let host = url.host, !host.isEmpty,
              url.user == nil, url.password == nil, url.query == nil, url.fragment == nil,
              url.path.isEmpty || url.path == "/", url.port.map({ (1...65535).contains($0) }) ?? true else { return nil }
        url.scheme = url.scheme == "https" ? "wss" : "ws"
        url.path = "/ws"
        return url.url
    }
    func save(server: String, key: String) -> Bool {
        let cleanServer = server.trimmingCharacters(in: .whitespacesAndNewlines)
        let cleanKey = key.trimmingCharacters(in: .whitespacesAndNewlines)
        guard Self.socketURL(cleanServer) != nil else { error = "请输入服务器地址，例如 http://127.0.0.1:8787"; return false }
        guard !cleanKey.isEmpty, cleanKey.utf8.allSatisfy({ $0 >= 33 && $0 <= 126 }) else { error = "请输入有效密钥（不含空格的 ASCII 字符）"; return false }
        let next = Configuration(server: cleanServer, key: cleanKey)
        do { try persist(next, name: "config.json") }
        catch { self.error = "无法保存配置：\(error.localizedDescription)"; return false }
        configuration = next
        error = nil
        connect()
        return true
    }
    private func persist<T: Encodable>(_ value: T, name: String) throws {
        let url = directory.appendingPathComponent(name)
        let data = try JSONEncoder().encode(value)
        // Directory is owner-only; enforce file permissions after atomic replacement.
        try data.write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
    func delete(_ id: String) {
        let next = messages.filter { $0.id != id }
        do { try persist(next, name: "messages.json") }
        catch { self.error = "无法删除消息：\(error.localizedDescription)"; return }
        messages = next
        if selected == id { selected = messages.first?.id }
    }
    func connect() {
        disconnect()
        guard !configuration.key.isEmpty, let url = Self.socketURL(configuration.server) else {
            state = "未配置"; return
        }
        let token = generation
        connectionLoop = Task { [weak self] in
            guard let self else { return }
            var attempt = 0
            while !Task.isCancelled && generation == token {
                state = attempt == 0 ? "连接中" : "重新连接中"
                var request = URLRequest(url: url)
                request.setValue("Bearer \(configuration.key)", forHTTPHeaderField: "Authorization")
                let task = session.webSocketTask(with: request)
                socket = task
                let started = Date()
                task.resume()
                let pingLoop = Task { [weak self, weak task] in
                    while !Task.isCancelled {
                        do { try await Task.sleep(nanoseconds: 15_000_000_000) } catch { return }
                        guard let self, let task, self.socket === task else { return }
                        // A missed pong cancels the receive operation and enters reconnect.
                        let deadline = Task { [weak task] in
                            do { try await Task.sleep(nanoseconds: 10_000_000_000) } catch { return }
                            task?.cancel(with: .goingAway, reason: nil)
                        }
                        task.sendPing { error in
                            deadline.cancel()
                            if error != nil { task.cancel(with: .goingAway, reason: nil) }
                        }
                    }
                }
                heartbeat = pingLoop
                do {
                    while !Task.isCancelled && generation == token {
                        let incoming = try await task.receive()
                        guard generation == token else { return }
                        let data: Data
                        switch incoming {
                        case .string(let text): data = Data(text.utf8)
                        case .data(let bytes): data = bytes
                        @unknown default: continue
                        }
                        guard data.count <= 16384, let notice = try? JSONDecoder().decode(Notice.self, from: data),
                              notice.title.unicodeScalars.count <= 30, notice.description.unicodeScalars.count <= 500 else { continue }
                        try receive(notice)
                        let ack = try JSONSerialization.data(withJSONObject: ["type": "ack", "id": notice.id])
                        try await task.send(.string(String(decoding: ack, as: UTF8.self)))
                    }
                } catch { /* Expected on disconnect; reconnect below. */ }
                pingLoop.cancel()
                task.cancel(with: .goingAway, reason: nil)
                guard !Task.isCancelled && generation == token else { return }
                connected = false
                let code = (task.response as? HTTPURLResponse)?.statusCode
                if code == 401 || code == 403 {
                    state = "鉴权失败"
                    error = "密钥不正确，请在设置中修改。"
                    return
                }
                if Date().timeIntervalSince(started) > 60 { attempt = 0 }
                let seconds = min(pow(2.0, Double(attempt)), 30)
                state = "已断开 · \(Int(seconds)) 秒后重连"
                attempt = min(attempt + 1, 5)
                do { try await Task.sleep(nanoseconds: UInt64((seconds + Double.random(in: 0...0.3)) * 1_000_000_000)) }
                catch { return }
            }
        }
    }
    func disconnect() {
        generation = UUID()
        connectionLoop?.cancel()
        heartbeat?.cancel()
        socket?.cancel(with: .goingAway, reason: nil)
        socket = nil
        connected = false
    }
    func receive(_ notice: Notice) throws {
        // Existing IDs were persisted successfully; replay only needs another ACK.
        guard !messages.contains(where: { $0.id == notice.id }) else { return }
        let next = [notice] + messages
        do { try persist(next, name: "messages.json") }
        catch {
            self.error = "无法保存消息，将在重连后重试：\(error.localizedDescription)"
            throw error
        }
        if error?.hasPrefix("无法保存消息") == true { error = nil }
        messages = next
        if selected == nil { selected = notice.id }
        guard deliverSystemNotifications else { return }
        let content = UNMutableNotificationContent()
        content.title = notice.title
        content.body = notice.description
        content.sound = .default
        content.userInfo = ["id": notice.id]
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: notice.id, content: content, trigger: nil))
    }
    nonisolated func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didOpenWithProtocol protocol: String?) {
        Task { @MainActor in
            guard self.socket === webSocketTask else { return }
            self.connected = true
            self.state = "已连接"
        }
    }
    nonisolated func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {}
}
