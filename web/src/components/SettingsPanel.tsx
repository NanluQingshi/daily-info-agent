import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { getApiToken, setApiToken } from "../api/client";

export function SettingsPanel() {
  const [token, setToken] = useState(getApiToken());
  const [saved, setSaved] = useState(false);

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

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">设置</h1>
        <p className="text-sm text-muted-foreground mt-1">
          当后端启用 <code className="text-foreground">CHAT_API_TOKEN</code> 时，在此填入对应 token 才能使用问答接口。
        </p>
      </div>

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
    </div>
  );
}
