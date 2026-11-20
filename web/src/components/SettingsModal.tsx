import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ModelSettings, ModelSettingsInput } from '../types'
import { displayLLM } from './Layout'
import { Modal } from './Modal'

type Draft = {
  id: string
  name: string
  base_url: string
  model: string
  api_key: string
  showKey: boolean
  clearKey: boolean
  has_key: boolean
  api_key_masked: string
}

function newId() {
  return 'm' + Math.random().toString(36).slice(2, 8)
}

function draftsFrom(s: ModelSettings): Draft[] {
  if (Array.isArray(s.models)) {
    return s.models.map((m) => ({
      id: m.id,
      name: m.name ?? '',
      base_url: m.base_url,
      model: m.model,
      api_key: '',
      showKey: false,
      clearKey: false,
      has_key: m.has_key,
      api_key_masked: m.api_key_masked,
    }))
  }
  return [
    {
      id: s.active_id || 'm1',
      name: '',
      base_url: s.base_url,
      model: s.model,
      api_key: '',
      showKey: false,
      clearKey: false,
      has_key: s.has_key,
      api_key_masked: s.api_key_masked,
    },
  ]
}

function emptyDraft(): Draft {
  return {
    id: newId(),
    name: '',
    base_url: '',
    model: '',
    api_key: '',
    showKey: false,
    clearKey: false,
    has_key: false,
    api_key_masked: '',
  }
}

/**
 * 设置：Tushare Token + 多条模型。选中的那条保存后即生效。
 */
export function SettingsModal({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [cur, setCur] = useState<ModelSettings | null>(null)
  const [drafts, setDrafts] = useState<Draft[]>([])
  const [activeId, setActiveId] = useState('')
  const [tsKey, setTsKey] = useState('')
  const [showTsKey, setShowTsKey] = useState(false)
  const [clearTsToken, setClearTsToken] = useState(false)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<'' | 'save' | string>('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [test, setTest] = useState<{ id: string; engine: string; latency: number } | null>(null)

  useEffect(() => {
    api
      .settings()
      .then((s) => {
        setCur(s)
        const ds = draftsFrom(s)
        setDrafts(ds)
        setActiveId(s.active_id || ds[0]?.id || '')
      })
      .catch((e) => setErr((e as Error).message))
      .finally(() => setLoading(false))
  }, [])

  const patch = (id: string, p: Partial<Draft>) => {
    setDrafts((list) => list.map((d) => (d.id === id ? { ...d, ...p } : d)))
    setMsg('')
    setErr('')
    setTest(null)
  }

  const payload = (useId = activeId): ModelSettingsInput => {
    const filled = drafts.filter(
      (d) => d.base_url.trim() || d.model.trim() || d.api_key.trim() || d.name.trim() || d.has_key,
    )
    const act = filled.find((d) => d.id === useId) ?? filled[0]
    return {
      base_url: act?.base_url ?? '',
      model: act?.model ?? '',
      temperature: cur?.temperature ?? 0.3,
      timeout_sec: cur?.timeout_sec ?? 90,
      api_key: act?.api_key.trim() || undefined,
      clear_key: act && act.clearKey && !act.api_key.trim() ? true : undefined,
      tushare_token: tsKey.trim() || undefined,
      clear_tushare_token: clearTsToken && !tsKey.trim() ? true : undefined,
      active_id: act?.id ?? useId,
      models: filled.map((d) => ({
        id: d.id,
        name: d.name.trim(),
        base_url: d.base_url.trim(),
        model: d.model.trim(),
        api_key: d.api_key.trim() || undefined,
        clear_key: d.clearKey && !d.api_key.trim() ? true : undefined,
      })),
    }
  }

  async function runTest(id: string) {
    setBusy(id)
    setErr('')
    setMsg('')
    setTest(null)
    try {
      const r = await api.testSettings(payload(id))
      setTest({ id, engine: r.engine, latency: r.latency_ms })
      setMsg('连通正常')
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function save() {
    setBusy('save')
    setErr('')
    setMsg('')
    try {
      const s = await api.saveSettings(payload())
      setCur(s)
      setDrafts(draftsFrom(s))
      setActiveId(s.active_id || '')
      setTsKey('')
      setClearTsToken(false)
      if (s.reload_error) {
        setErr('配置已保存，但加载行情失败：' + s.reload_error)
      } else {
        setMsg('已保存')
      }
      onSaved()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  function addModel() {
    const d = emptyDraft()
    setDrafts((list) => [...list, d])
    setActiveId(d.id)
    setMsg('')
    setErr('')
    setTest(null)
  }

  function removeModel(id: string) {
    setDrafts((list) => {
      const next = list.filter((d) => d.id !== id)
      if (activeId === id) setActiveId(next[0]?.id || '')
      return next
    })
    setMsg('')
    setErr('')
    setTest(null)
  }

  return (
    <Modal
      title="设置"
      onClose={onClose}
      width={780}
      footer={
        <>
          <span className="grow" />
          <button className="btn" onClick={onClose} disabled={busy !== ''}>
            取消
          </button>
          <button className="btn primary" onClick={save} disabled={busy !== '' || loading}>
            {busy === 'save' ? '保存中…' : '保存'}
          </button>
        </>
      }
    >
      <div className="set-sec">Tushare Token</div>
      <div className="field">
        <div className="row tight" style={{ gap: 6 }}>
          <input
            type={showTsKey ? 'text' : 'password'}
            value={tsKey}
            placeholder={cur?.has_tushare_token ? `已保存 ${cur.tushare_token_masked}` : '在 tushare.pro 申请'}
            spellCheck={false}
            autoComplete="off"
            onChange={(e) => {
              setTsKey(e.target.value)
              setClearTsToken(false)
              setMsg('')
              setErr('')
            }}
          />
          <button className="btn sm" onClick={() => setShowTsKey(!showTsKey)}>
            {showTsKey ? '隐藏' : '显示'}
          </button>
          {cur?.has_tushare_token && (
            <button
              className="btn sm"
              onClick={() => {
                setClearTsToken(true)
                setTsKey('')
              }}
            >
              清除
            </button>
          )}
        </div>
      </div>

      <div className="set-sec">
        模型
        <button className="btn xs" onClick={addModel} disabled={busy !== ''}>
          添加
        </button>
      </div>

      {drafts.length === 0 && <div className="subtle">尚未添加模型。点「添加」配置一条。</div>}

      {drafts.length > 0 && (
        <div className="tbl-wrap set-models">
          <table className="tbl">
            <thead>
              <tr>
                <th className="set-col-pick" title="当前使用" />
                <th>访问地址</th>
                <th className="set-col-model">模型名</th>
                <th className="set-col-key">密钥</th>
                <th className="set-col-ops" />
              </tr>
            </thead>
            <tbody>
              {drafts.map((d) => {
                const on = d.id === activeId
                return (
                  <tr key={d.id} className={on ? 'on' : undefined}>
                    <td className="set-col-pick">
                      <input
                        type="radio"
                        name="active-model"
                        checked={on}
                        title={on ? '当前使用' : '设为当前'}
                        onChange={() => setActiveId(d.id)}
                      />
                    </td>
                    <td>
                      <input
                        type="text"
                        value={d.base_url}
                        placeholder="https://api.deepseek.com/v1"
                        spellCheck={false}
                        onChange={(e) => patch(d.id, { base_url: e.target.value })}
                      />
                    </td>
                    <td>
                      <input
                        type="text"
                        value={d.model}
                        placeholder="deepseek-v4-flash"
                        spellCheck={false}
                        onChange={(e) => patch(d.id, { model: e.target.value })}
                      />
                    </td>
                    <td>
                      <div className="set-key">
                        <input
                          type={d.showKey ? 'text' : 'password'}
                          value={d.api_key}
                          placeholder={d.has_key && !d.clearKey ? d.api_key_masked : 'API Key'}
                          spellCheck={false}
                          autoComplete="off"
                          onChange={(e) => patch(d.id, { api_key: e.target.value, clearKey: false })}
                        />
                        <button
                          type="button"
                          className="set-key-toggle"
                          onClick={() => patch(d.id, { showKey: !d.showKey })}
                        >
                          {d.showKey ? '隐藏' : '显示'}
                        </button>
                      </div>
                    </td>
                    <td className="ops">
                      <button className="btn xs" onClick={() => runTest(d.id)} disabled={busy !== '' || loading}>
                        {busy === d.id ? '测试中…' : '测试'}
                      </button>
                      <button className="btn xs" onClick={() => removeModel(d.id)} disabled={busy !== ''}>
                        删除
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {test && (
        <div className="subtle" style={{ marginTop: 6 }}>
          {displayLLM(test.engine) || test.engine} · {test.latency} ms
        </div>
      )}

      {loading && <div className="busy-line">读取配置…</div>}
      {msg && (
        <div className="note ok">
          <span>✓</span>
          <div>{msg}</div>
        </div>
      )}
      {err && (
        <div className="note err">
          <span>⚠</span>
          <div>{err}</div>
        </div>
      )}
    </Modal>
  )
}
