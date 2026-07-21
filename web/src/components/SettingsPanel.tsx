import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { getApiToken, setApiToken } from "../api/client";
import { clearChatHistory } from "./ChatView";

export function SettingsPanel() {
  const [token, setToken] = useState(getApiToken());
  const [saved, setSaved] = useState(false);
  const [cleared, setCleared] = useState(false);
  const [confirmClear, setConfirmClear] = useState(false);

  const save = () => {
    setApiToken(token);
    setSaved(true);
    window.setTimeout(() => setSaved(false), 2000);
  };

  const clear = () => {
    setApiToken("");
    setToken("");
    setSaved(false);
  };

  const handleClearChat = () => {
    clearChatHistory();
    setCleared(true);
    setConfirmClear(false);
    window.setTimeout(() => setCleared(false), 2000);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">设置</h1>
        <p className="text-sm text-muted-foreground mt-1">
          应用配置项，所有设置保存在浏览器本地。
        </p>
      </div>

      {/* Chat API Token */}
      <div className="space-y-2">
        <label className="text-sm font-medium">Chat API Token</label>
        <Input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="留空表示不启用鉴权"
          autoComplete="off"
        />
        <p className="text-xs text-muted-foreground">
          Token 保存在浏览器 localStorage，仅本机生效。
        </p>
      </div>

      <div className="flex items-center gap-3">
        <Button onClick={save}>保存</Button>
        <Button variant="outline" onClick={clear}>清除</Button>
        {saved && <span className="text-sm text-muted-foreground">已保存</span>}
      </div>

      <Separator />

      {/* Chat History */}
      <div className="space-y-2">
        <label className="text-sm font-medium">对话历史</label>
        <p className="text-xs text-muted-foreground">
          清除所有本地保存的对话记录和消息，此操作不可撤销。
        </p>
      </div>

      <div className="flex items-center gap-3">
        {confirmClear ? (
          <>
            <Button variant="destructive" size="sm" onClick={handleClearChat}>
              确认清除
            </Button>
            <Button variant="outline" size="sm" onClick={() => setConfirmClear(false)}>
              取消
            </Button>
          </>
        ) : (
          <Button variant="outline" size="sm" onClick={() => setConfirmClear(true)}>
            清除对话历史
          </Button>
        )}
        {cleared && <span className="text-sm text-muted-foreground">已清除</span>}
      </div>
    </div>
  );
}
