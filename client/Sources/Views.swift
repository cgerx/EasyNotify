import SwiftUI
import WebKit
import UserNotifications

struct MainView: View {
    @ObservedObject var store: Store
    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "bell.badge").font(.title3).foregroundStyle(Color.accentColor)
                Text("EasyNotify").font(.system(size: 16, weight: .semibold))
                Spacer()
                Circle().fill(store.connected ? Color.green : Color.orange).frame(width: 7, height: 7)
                Text(store.state).font(.system(size: 12)).foregroundStyle(.secondary)
                Button { store.settingsVisible = true } label: { Image(systemName: "gearshape") }
                    .buttonStyle(.borderless).help("设置").accessibilityLabel("设置")
            }.padding(.horizontal, 22).padding(.vertical, 16)
            Divider()
            HSplitView {
                VStack(spacing: 0) {
                    HStack { Text("通知").font(.system(size: 12, weight: .semibold)); Spacer(); Text("\(store.messages.count)").font(.system(size: 12)).foregroundStyle(.secondary) }
                        .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 8)
                    if store.messages.isEmpty {
                        VStack(spacing: 10) {
                            Image(systemName: "tray").font(.system(size: 28, weight: .light))
                            Text("暂无通知").font(.system(size: 13))
                        }.foregroundStyle(.secondary).frame(maxWidth: .infinity, maxHeight: .infinity)
                    } else {
                        List(selection: $store.selected) {
                            ForEach(store.messages) { notice in
                                VStack(alignment: .leading, spacing: 7) {
                                    Text(notice.title).font(.system(size: 13, weight: .semibold)).lineLimit(2)
                                    Text(notice.description.replacingOccurrences(of: "\n", with: " "))
                                        .font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(2)
                                    Text(notice.date, format: .dateTime.month().day().hour().minute())
                                        .font(.system(size: 10)).foregroundStyle(.tertiary)
                                }.padding(.vertical, 7).tag(notice.id)
                                    .contextMenu { Button("删除", role: .destructive) { store.delete(notice.id) } }
                            }
                        }.listStyle(.sidebar)
                        .onDeleteCommand { if let id = store.selected { store.delete(id) } }
                    }
                }.frame(minWidth: 220, idealWidth: 270, maxWidth: 340)
                if let notice = store.messages.first(where: { $0.id == store.selected }) {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(alignment: .top) {
                            VStack(alignment: .leading, spacing: 9) {
                                Text(notice.title).font(.system(size: 24, weight: .semibold)).textSelection(.enabled)
                                Text(notice.date, format: .dateTime.year().month().day().hour().minute().second())
                                    .font(.system(size: 11)).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button { store.delete(notice.id) } label: { Image(systemName: "trash") }
                                .buttonStyle(.borderless).help("删除消息").accessibilityLabel("删除消息")
                        }
                        Divider()
                        MarkdownView(markdown: notice.description)
                    }.padding(26).frame(minWidth: 350, maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    VStack(spacing: 12) {
                        Image(systemName: "text.bubble").font(.system(size: 38, weight: .ultraLight))
                        Text(store.messages.isEmpty ? "收到的通知会显示在这里" : "选择一条通知").font(.system(size: 13))
                        if store.configuration.key.isEmpty { Button("连接服务器") { store.settingsVisible = true }.padding(.top, 8) }
                    }.foregroundStyle(.secondary).frame(minWidth: 350, maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            if let error = store.error {
                Divider()
                HStack { Image(systemName: "exclamationmark.circle"); Text(error).textSelection(.enabled); Spacer(); Button("关闭") { store.error = nil } }
                    .font(.system(size: 12)).foregroundStyle(.red).padding(12)
            }
        }.frame(minWidth: 700, minHeight: 460)
        .sheet(isPresented: $store.settingsVisible) { SettingsView(store: store) }
    }
}

struct SettingsView: View {
    @ObservedObject var store: Store
    @State private var server = ""
    @State private var key = ""
    @State private var notificationStatus = ""
    @StateObject private var loginItem = LoginItem()
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("设置").font(.title2.weight(.semibold))
            VStack(alignment: .leading, spacing: 8) {
                Text("服务器").font(.system(size: 12, weight: .medium))
                TextField("http://127.0.0.1:8787", text: $server).textFieldStyle(.roundedBorder)
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("密钥").font(.system(size: 12, weight: .medium))
                SecureField("服务器接收密钥", text: $key).textFieldStyle(.roundedBorder)
            }
            Toggle("登录时启动", isOn: Binding(
                get: { loginItem.enabled },
                set: { loginItem.setEnabled($0) }
            ))
            .toggleStyle(.switch)
            .disabled(loginItem.updating)
            if loginItem.needsApproval {
                HStack {
                    Text("请在系统设置中允许登录启动").font(.caption).foregroundStyle(.secondary)
                    Spacer()
                    Button("打开系统设置") { loginItem.openSystemSettings() }
                }
            }
            if let error = loginItem.error {
                Text(error).font(.caption).foregroundStyle(.red)
            }
            HStack {
                Button("启用系统通知") {
                    UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
                        Task { @MainActor in notificationStatus = granted ? "已启用" : "请在系统设置 → 通知中允许 EasyNotify" }
                    }
                }
                Text(notificationStatus).font(.caption).foregroundStyle(.secondary)
            }
            if let error = store.error { Text(error).font(.caption).foregroundStyle(.red) }
            HStack {
                Spacer()
                Button("取消") { store.settingsVisible = false }.keyboardShortcut(.cancelAction)
                Button("保存并连接") { if store.save(server: server, key: key) { store.settingsVisible = false } }
                    .keyboardShortcut(.defaultAction)
            }
        }.padding(26).frame(width: 430)
        .onAppear { server = store.configuration.server; key = store.configuration.key; loginItem.refresh() }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            if !loginItem.updating { loginItem.refresh() }
        }
    }
}

struct MarkdownView: NSViewRepresentable {
    let markdown: String
    func makeCoordinator() -> Coordinator { Coordinator() }
    func makeNSView(context: Context) -> WKWebView {
        let view = WKWebView(frame: .zero)
        view.setValue(false, forKey: "drawsBackground")
        view.navigationDelegate = context.coordinator
        context.coordinator.markdown = markdown
        let bundle = Bundle.main
        if let path = bundle.url(forResource: "reader", withExtension: "html"),
           var html = try? String(contentsOf: path, encoding: .utf8),
           let markedURL = bundle.url(forResource: "marked", withExtension: "js"),
           let purifyURL = bundle.url(forResource: "purify", withExtension: "js"),
           let marked = try? String(contentsOf: markedURL, encoding: .utf8),
           let purify = try? String(contentsOf: purifyURL, encoding: .utf8) {
            html = html.replacingOccurrences(of: "/*MARKED*/", with: marked)
                .replacingOccurrences(of: "/*PURIFY*/", with: purify)
            view.loadHTMLString(html, baseURL: nil)
        }
        return view
    }
    func updateNSView(_ view: WKWebView, context: Context) {
        guard context.coordinator.markdown != markdown else { return }
        context.coordinator.markdown = markdown
        if context.coordinator.ready { context.coordinator.render(view) }
    }
    final class Coordinator: NSObject, WKNavigationDelegate {
        var markdown = ""
        var ready = false
        func render(_ view: WKWebView) {
            let encoded = Data(markdown.utf8).base64EncodedString()
            view.evaluateJavaScript("window.setMarkdown('\(encoded)')")
        }
        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { ready = true; render(webView) }
        func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            if navigationAction.navigationType == .linkActivated {
                if let url = navigationAction.request.url, ["http", "https", "mailto"].contains(url.scheme?.lowercased() ?? "") { NSWorkspace.shared.open(url) }
                decisionHandler(.cancel)
            } else { decisionHandler(navigationAction.request.url?.scheme == "about" ? .allow : .cancel) }
        }
    }
}
