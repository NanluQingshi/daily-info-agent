import { useState } from "react";
import { ArticleList } from "./components/ArticleList";
import { ChatPanel } from "./components/ChatPanel";
import { StatsPanel } from "./components/StatsPanel";

type Tab = "articles" | "chat" | "stats";

const TABS: { id: Tab; label: string }[] = [
  { id: "articles", label: "文章管理" },
  { id: "chat", label: "智能问答" },
  { id: "stats", label: "统计" },
];

export default function App() {
  const [tab, setTab] = useState<Tab>("articles");

  return (
    <div className="min-h-screen bg-slate-50">
      <header className="bg-white border-b border-slate-200">
        <div className="max-w-5xl mx-auto px-4 flex items-center gap-6 h-14">
          <span className="font-semibold text-slate-900 text-sm">Daily Info Agent</span>
          <nav className="flex gap-1">
            {TABS.map((t) => (
              <button
                key={t.id}
                onClick={() => setTab(t.id)}
                className={`px-3 py-1.5 text-sm rounded-lg transition-colors ${
                  tab === t.id
                    ? "bg-blue-50 text-blue-700 font-medium"
                    : "text-slate-500 hover:text-slate-700 hover:bg-slate-100"
                }`}
              >
                {t.label}
              </button>
            ))}
          </nav>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-4 py-6">
        {tab === "articles" && <ArticleList />}
        {tab === "chat" && <ChatPanel />}
        {tab === "stats" && <StatsPanel />}
      </main>
    </div>
  );
}
