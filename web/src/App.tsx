import { useState } from "react";
import { ArticleList } from "./components/ArticleList";
import { ChatPanel } from "./components/ChatPanel";
import { StatsPanel } from "./components/StatsPanel";

type Tab = "chat" | "articles" | "stats";

const NAV: { id: Tab; label: string; icon: string }[] = [
  { id: "chat",     label: "智能问答", icon: "💬" },
  { id: "articles", label: "文章管理", icon: "📰" },
  { id: "stats",    label: "统计",     icon: "📊" },
];

export default function App() {
  const [tab, setTab] = useState<Tab>("chat");

  return (
    <div className="flex h-screen bg-slate-50 overflow-hidden">
      {/* Sidebar */}
      <aside className="w-52 shrink-0 bg-white border-r border-slate-200 flex flex-col">
        <div className="h-14 flex items-center px-4 border-b border-slate-100 shrink-0">
          <span className="font-semibold text-sm text-slate-900">Daily Info Agent</span>
        </div>
        <nav className="flex-1 p-2 space-y-0.5 overflow-y-auto">
          {NAV.map((item) => (
            <button
              key={item.id}
              onClick={() => setTab(item.id)}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-colors text-left ${
                tab === item.id
                  ? "bg-blue-50 text-blue-700 font-medium"
                  : "text-slate-600 hover:bg-slate-100 hover:text-slate-900"
              }`}
            >
              <span>{item.icon}</span>
              {item.label}
            </button>
          ))}
        </nav>
      </aside>

      {/* Main */}
      <main className="flex-1 overflow-hidden">
        {tab === "chat" && <ChatPanel />}
        {tab === "articles" && (
          <div className="h-full overflow-y-auto">
            <div className="max-w-5xl mx-auto px-6 py-6">
              <ArticleList />
            </div>
          </div>
        )}
        {tab === "stats" && (
          <div className="h-full overflow-y-auto">
            <div className="max-w-4xl mx-auto px-6 py-6">
              <StatsPanel />
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
