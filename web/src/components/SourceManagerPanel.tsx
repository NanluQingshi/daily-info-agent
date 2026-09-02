import { useCallback, useEffect, useState } from "react";
import { Power, Plus, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { addSource, listSources, removeSource, setSourceEnabled } from "../api/client";
import type { SourceRow } from "../types";
import { showToast } from "./Toast";

/**
 * Managed RSS sources (issue #80): add / pause / remove sources at runtime.
 * Changes apply on the next pipeline run — no restart needed. An empty list
 * means the server is still running on the static RSS_FEEDS env list.
 */
export function SourceManagerPanel({ onChanged }: { onChanged?: () => void }) {
  const [rows, setRows] = useState<SourceRow[] | null>(null);
  const [url, setUrl] = useState("");
  const [loading, setLoading] = useState(false);
  const [busyId, setBusyId] = useState<number | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    listSources()
      .then((resp) => setRows(resp.sources ?? []))
      .catch((e: Error) => {
        showToast("error", `源列表加载失败: ${e.message}`);
        setRows((prev) => prev ?? []);
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const add = async () => {
    const trimmed = url.trim();
    if (!trimmed) return;
    setBusyId(-1);
    try {
      await addSource(trimmed);
      setUrl("");
      showToast("success", "源已添加，下一轮抓取生效");
      load();
      onChanged?.();
    } catch (e) {
      showToast("error", `添加失败: ${(e as Error).message}`);
    } finally {
      setBusyId(null);
    }
  };

  const toggle = async (row: SourceRow, enabled: boolean) => {
    setBusyId(row.id);
    try {
      await setSourceEnabled(row.id, enabled);
      setRows((prev) =>
        prev ? prev.map((r) => (r.id === row.id ? { ...r, enabled } : r)) : prev,
      );
      showToast("success", enabled ? "源已启用" : "源已停用");
      onChanged?.();
    } catch (e) {
      showToast("error", `操作失败: ${(e as Error).message}`);
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (row: SourceRow) => {
    setBusyId(row.id);
    try {
      await removeSource(row.id);
      setRows((prev) => (prev ? prev.filter((r) => r.id !== row.id) : prev));
      showToast("success", "源已删除（历史文章保留）");
      onChanged?.();
    } catch (e) {
      showToast("error", `删除失败: ${(e as Error).message}`);
    } finally {
      setBusyId(null);
    }
  };

  const enabledCount = rows?.filter((r) => r.enabled).length ?? 0;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium">
            数据源管理
            {rows && rows.length > 0 && (
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                共 {rows.length} 个 · {enabledCount} 个启用
              </span>
            )}
          </CardTitle>
          <Button variant="ghost" size="sm" onClick={load} disabled={loading} className="h-7 px-2">
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex gap-2">
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && add()}
            placeholder="https://example.com/rss — 添加新的 RSS 源"
            className="h-8 text-xs font-mono"
            disabled={busyId === -1}
          />
          <Button size="sm" className="h-8 px-3" onClick={add} disabled={!url.trim() || busyId === -1}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            添加
          </Button>
        </div>

        {rows === null ? (
          <p className="text-sm text-muted-foreground text-center py-4">加载中…</p>
        ) : rows.length === 0 ? (
          <p className="text-xs text-muted-foreground text-center py-3">
            尚未管理任何源 — 首次启动后 RSS_FEEDS 环境变量中的源会自动导入到这里
          </p>
        ) : (
          <ul className="divide-y">
            {rows.map((row) => (
              <li key={row.id} className="flex items-center gap-3 py-2">
                <Button
                  variant={row.enabled ? "default" : "outline"}
                  size="sm"
                  className="h-7 px-2.5 text-xs"
                  onClick={() => toggle(row, !row.enabled)}
                  disabled={busyId === row.id}
                  aria-label={row.enabled ? "停用该源" : "启用该源"}
                  title={row.enabled ? "点击停用" : "点击启用"}
                >
                  <Power className="w-3 h-3 mr-1" />
                  {row.enabled ? "启用中" : "已停用"}
                </Button>
                <span
                  className={`flex-1 font-mono text-[11px] truncate ${
                    row.enabled ? "" : "text-muted-foreground line-through"
                  }`}
                  title={row.url}
                >
                  {row.url}
                </span>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-red-600 dark:text-red-400 hover:text-red-700"
                  onClick={() => remove(row)}
                  disabled={busyId === row.id}
                  aria-label="删除该源"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <p className="text-[10px] text-muted-foreground">
          增删改无需重启，下一轮抓取自动生效；停用的源保留健康记录，删除的源已入库文章不受影响。
        </p>
      </CardContent>
    </Card>
  );
}
