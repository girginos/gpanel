// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import ConfirmDialog from '@/components/ConfirmDialog'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ipv4: string }
type Repo = {
  id: number; domain_id: number; repo_url: string; branch: string; target_dir: string;
  deploy_key_pub: string; webhook_secret: string; son_sync?: string; son_commit?: string; son_durum: string; olusturulma: string
}
type GHConn = {
  yok?: boolean
  login?: string; ad_soyad?: string; avatar_url?: string
  secili_repo?: string; secili_branch?: string
  webhook_id?: number; webhook_url?: string
}
type GHRepo = { full_name: string; name: string; description?: string; private: boolean; default_branch: string; updated_at: string }

export default function DomainGitPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [repo, setRepo] = useState<Repo | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [silinecek, setSilinecek] = useState(false)
  const [klonOnay, setKlonOnay] = useState(false)
  const [logSon, setLogSon] = useState<string | null>(null)

  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('main')
  const [targetDir, setTargetDir] = useState('public_html')

  // GitHub connector state
  const [ghConn, setGhConn] = useState<GHConn>({ yok: true })
  const [ghToken, setGhToken] = useState('')
  const [ghRepos, setGhRepos] = useState<GHRepo[]>([])
  const [ghBranches, setGhBranches] = useState<string[]>([])
  const [ghSelectedRepo, setGhSelectedRepo] = useState('')
  const [ghSelectedBranch, setGhSelectedBranch] = useState('')
  const [ghAutoDeploy, setGhAutoDeploy] = useState(true)
  const [ghYukluyor, setGhYukluyor] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
    api.get<Repo | null>(`/domains/${id}/git`)
      .then(r => { setRepo(r.data); if (r.data) { setRepoUrl(r.data.repo_url); setBranch(r.data.branch); setTargetDir(r.data.target_dir) } })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
    api.get<GHConn>(`/domains/${id}/github`).then(r => {
      setGhConn(r.data)
      if (!r.data.yok) {
        if (r.data.secili_repo) setGhSelectedRepo(r.data.secili_repo)
        if (r.data.secili_branch) setGhSelectedBranch(r.data.secili_branch)
        ghLoadRepos()
      }
    }).catch(() => {})
  }
  useEffect(yukle, [id])

  async function ghConnect() {
    if (!ghToken.trim()) { setHata('GitHub PAT zorunlu'); return }
    setGhYukluyor(true); setHata(null); setBasari(null)
    try {
      const r = await api.post<GHConn>(`/domains/${id}/github/connect`, { token: ghToken.trim() })
      setGhConn(r.data); setGhToken('')
      setBasari(`GitHub bağlandı: @${r.data.login}`)
      ghLoadRepos()
    } catch (e) {
      setHata(apiHata(e, 'GitHub bağlantı başarısız'))
    } finally { setGhYukluyor(false) }
  }

  async function ghLoadRepos() {
    try {
      const r = await api.get<GHRepo[]>(`/domains/${id}/github/repos`)
      setGhRepos(r.data || [])
    } catch (e) {
      setHata(apiHata(e, 'Repo listesi alınamadı'))
    }
  }

  async function ghLoadBranches(repoFull: string) {
    setGhSelectedRepo(repoFull)
    const rp = ghRepos.find(x => x.full_name === repoFull)
    if (rp) setGhSelectedBranch(rp.default_branch || 'main')
    try {
      const r = await api.get<string[]>(`/domains/${id}/github/branches?repo=${encodeURIComponent(repoFull)}`)
      setGhBranches(r.data || [])
    } catch {
      setGhBranches([])
    }
  }

  async function ghUse() {
    if (!ghSelectedRepo || !ghSelectedBranch) { setHata('Repo ve branch seçin'); return }
    setGhYukluyor(true); setHata(null); setBasari(null)
    try {
      const r = await api.post<{ ok: boolean; webhook_ok?: boolean; webhook_hata?: string }>(
        `/domains/${id}/github/use`,
        { repo: ghSelectedRepo, branch: ghSelectedBranch, target_dir: targetDir, auto_deploy: ghAutoDeploy }
      )
      let msg = `Repo bağlandı: ${ghSelectedRepo}@${ghSelectedBranch}`
      if (ghAutoDeploy) {
        if (r.data.webhook_ok) msg += ' · Otomatik dağıtım aktif (webhook kuruldu)'
        else if (r.data.webhook_hata) msg += ` · Webhook hata: ${r.data.webhook_hata}`
      }
      setBasari(msg)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Bağlama başarısız'))
    } finally { setGhYukluyor(false) }
  }

  async function ghDisconnect() {
    if (!confirm('GitHub bağlantısı kaldırılacak. Webhook varsa silinir, mevcut repo dosyaları etkilenmez.')) return
    try {
      await api.delete(`/domains/${id}/github`)
      setGhConn({ yok: true })
      setGhRepos([])
      setGhBranches([])
      setGhSelectedRepo('')
      setGhSelectedBranch('')
      setBasari('GitHub bağlantısı kaldırıldı')
    } catch (e) {
      setHata(apiHata(e))
    }
  }

  async function bagla() {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.post(`/domains/${id}/git`, { repo_url: repoUrl, branch, target_dir: targetDir })
      setBasari('Repo bağlandı. Deploy key\'i kopyalayıp GitHub repo\'nuza ekleyin, sonra "Klonla" tıklayın.')
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Bağlama başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  async function klonla() {
    setIsleniyor(true); setHata(null); setBasari(null); setLogSon(null); setKlonOnay(false)
    try {
      const { data } = await api.post(`/domains/${id}/git/klonla`)
      setBasari(`Klonlandı! Commit: ${data.commit.slice(0, 7)}`)
      setLogSon(data.log)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Klonlama başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  async function pull() {
    setIsleniyor(true); setHata(null); setBasari(null); setLogSon(null)
    try {
      const { data } = await api.post(`/domains/${id}/git/pull`)
      setBasari(`Pull tamam. Commit: ${data.commit.slice(0, 7)}`)
      setLogSon(data.log)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Pull başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  async function sil() {
    try {
      await api.delete(`/domains/${id}/git`)
      setRepo(null); setSilinecek(false); setBasari('Repo bağlantısı kaldırıldı')
    } catch (e) {
      alert(apiHata(e))
    }
  }

  function kopyala(s: string) {
    navigator.clipboard.writeText(s)
  }

  return (
    <div className="px-6 py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Git' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Git Deploy</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
        {' · '}Repo bağlayın, deploy key'i GitHub'a ekleyin, otomatik pull için webhook URL'ini kullanın.
      </p>}

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> : (
        <>
          {/* GitHub Connector */}
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-3">
                <svg viewBox="0 0 24 24" fill="currentColor" className="w-6 h-6 text-slate-900 dark:text-slate-100">
                  <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.4 3-.405 1.02.005 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/>
                </svg>
                <div>
                  <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">GitHub Bağlantısı</h3>
                  <p className="text-xs text-slate-500 dark:text-slate-500">PAT ile bağlan → repo'larını listele → tek tıkla dağıtım + otomatik webhook.</p>
                </div>
              </div>
              {ghConn.login && (
                <button onClick={ghDisconnect} className="text-xs px-2 py-1 border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 rounded">Bağlantıyı kaldır</button>
              )}
            </div>

            {ghConn.yok || !ghConn.login ? (
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Personal Access Token (classic)</label>
                <div className="flex gap-2">
                  <input type="password" value={ghToken}
                    onChange={e => setGhToken(e.target.value)}
                    placeholder="ghp_..." autoComplete="off"
                    className="flex-1 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
                  <button onClick={ghConnect} disabled={ghYukluyor || !ghToken.trim()}
                    className="px-4 py-2 bg-slate-900 hover:bg-slate-800 disabled:bg-slate-400 text-white text-sm font-medium rounded">
                    {ghYukluyor ? 'Bağlanıyor…' : 'Bağlan'}
                  </button>
                </div>
                <p className="text-[11px] text-slate-500 dark:text-slate-500 mt-2">
                  <a href="https://github.com/settings/tokens?type=beta" target="_blank" rel="noreferrer"
                    className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300">github.com/settings/tokens</a> →
                  Fine-grained PAT → scope: <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">repo</code> +
                  <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded ml-1">admin:repo_hook</code> (auto-deploy için).
                </p>
              </div>
            ) : (
              <div className="space-y-3">
                <div className="flex items-center gap-3 p-3 bg-slate-50 dark:bg-slate-900 rounded-md">
                  {ghConn.avatar_url && <img src={ghConn.avatar_url} alt="" className="w-10 h-10 rounded-full"/>}
                  <div className="flex-1">
                    <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{ghConn.ad_soyad || ghConn.login}</div>
                    <div className="text-xs text-slate-500 dark:text-slate-500 font-mono">@{ghConn.login}</div>
                  </div>
                  {ghConn.webhook_url && (
                    <span className="text-[10px] uppercase tracking-wider font-semibold px-2 py-1 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300">● Otomatik Dağıtım</span>
                  )}
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Repo</label>
                    <select value={ghSelectedRepo} onChange={e => ghLoadBranches(e.target.value)}
                      className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono bg-white dark:bg-slate-800">
                      <option value="">— seç —</option>
                      {ghRepos.map(r => (
                        <option key={r.full_name} value={r.full_name}>
                          {r.private ? '🔒 ' : ''}{r.full_name}
                        </option>
                      ))}
                    </select>
                    <span className="text-[10px] text-slate-500 dark:text-slate-500 mt-0.5 block">{ghRepos.length} repo bulundu</span>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Branch</label>
                    <select value={ghSelectedBranch} onChange={e => setGhSelectedBranch(e.target.value)}
                      disabled={!ghSelectedRepo}
                      className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono bg-white dark:bg-slate-800 disabled:bg-slate-50 dark:bg-slate-900">
                      {!ghSelectedBranch && <option value="">— seç —</option>}
                      {ghBranches.map(b => <option key={b} value={b}>{b}</option>)}
                      {ghSelectedBranch && !ghBranches.includes(ghSelectedBranch) && <option value={ghSelectedBranch}>{ghSelectedBranch}</option>}
                    </select>
                  </div>
                </div>

                <div className="flex items-center justify-between flex-wrap gap-2">
                  <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
                    <input type="checkbox" checked={ghAutoDeploy} onChange={e => setGhAutoDeploy(e.target.checked)} className="cursor-pointer"/>
                    Push'ta otomatik dağıtım (webhook kurulur)
                  </label>
                  <button onClick={ghUse} disabled={ghYukluyor || !ghSelectedRepo || !ghSelectedBranch}
                    className="px-4 py-2 bg-emerald-600 hover:bg-emerald-700 disabled:bg-emerald-300 text-white text-sm font-medium rounded">
                    {ghYukluyor ? 'Bağlanıyor…' : 'Bu Repo\'yu Kullan'}
                  </button>
                </div>

                {ghConn.webhook_url && (
                  <div className="text-[11px] text-slate-500 dark:text-slate-500 font-mono bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded p-2 truncate" title={ghConn.webhook_url}>
                    Webhook: {ghConn.webhook_url}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Repo bağlama / güncelleme */}
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
            <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3">{repo ? 'Repo Ayarları' : 'Repo Bağla'}</h3>
            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Git URL (SSH)</label>
                <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)}
                  placeholder="git@github.com:kullanici/repo.git"
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
                <p className="text-xs text-slate-500 dark:text-slate-500 mt-1">Private repo için SSH URL kullanın; HTTPS ile auth çalışmaz.</p>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Branch</label>
                  <input type="text" value={branch} onChange={e => setBranch(e.target.value)}
                    className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono" />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Hedef Dizin</label>
                  <input type="text" value={targetDir} onChange={e => setTargetDir(e.target.value)}
                    className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono" />
                </div>
              </div>
              <div className="flex gap-2">
                <button onClick={bagla} disabled={isleniyor || !repoUrl} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm font-medium rounded-md">
                  {repo ? 'Güncelle' : 'Bağla'}
                </button>
                {repo && (
                  <button onClick={() => setKlonOnay(true)} disabled={isleniyor} className="px-4 py-2 bg-amber-100 dark:bg-amber-900/30 hover:bg-amber-200 text-amber-800 dark:text-amber-200 text-sm font-medium rounded-md">
                    {isleniyor ? '...' : '⬇ Klonla (hedef temizlenir)'}
                  </button>
                )}
                {repo && (
                  <button onClick={pull} disabled={isleniyor} className="px-4 py-2 bg-emerald-600 hover:bg-emerald-700 disabled:bg-emerald-300 text-white text-sm font-medium rounded-md">
                    {isleniyor ? '...' : '↻ Pull'}
                  </button>
                )}
                {repo && (
                  <button onClick={() => setSilinecek(true)} className="ml-auto px-4 py-2 border border-red-300 dark:border-red-700 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 text-sm rounded-md">
                    Bağlantıyı Kaldır
                  </button>
                )}
              </div>
            </div>
          </div>

          {repo && (
            <>
              {/* Deploy key */}
              <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
                <div className="flex items-center justify-between mb-2">
                  <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">Deploy Key (Public)</h3>
                  <button onClick={() => kopyala(repo.deploy_key_pub)} className="text-xs px-2 py-1 bg-slate-100 dark:bg-slate-800 hover:bg-brand-100 dark:bg-brand-900/30 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 rounded">Kopyala</button>
                </div>
                <p className="text-xs text-slate-500 dark:text-slate-500 mb-2">GitHub → Repository → Settings → Deploy keys → Add deploy key — bu anahtarı yapıştırın (Allow write access GEREKMEZ).</p>
                <textarea readOnly value={repo.deploy_key_pub} rows={3}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md text-xs font-mono break-all" />
              </div>

              {/* Webhook */}
              <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
                <div className="flex items-center justify-between mb-2">
                  <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">Webhook (Otomatik Pull)</h3>
                </div>
                <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">GitHub → Repository → Settings → Webhooks → Add webhook. Content type: <code className="font-mono">application/json</code>. Push event yeterli.</p>
                <div className="space-y-2">
                  <Sat e="Payload URL" d={`http://${domain?.ipv4 || ''}:8443/api/v1/git-webhook/${repo.webhook_secret}`} onCopy={kopyala} />
                  <Sat e="Secret" d={repo.webhook_secret} onCopy={kopyala} />
                  <Sat e="Content type" d="application/json" />
                  <Sat e="Events" d="Just the push event" />
                </div>
              </div>

              {/* Durum */}
              <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
                <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3">Sync Durumu</h3>
                <div className="grid grid-cols-3 gap-3 text-sm">
                  <Stat e="Son sync" d={repo.son_sync || '— (henüz yok)'} />
                  <Stat e="Son commit" d={repo.son_commit ? repo.son_commit.slice(0, 8) : '—'} mono />
                  <Stat e="Durum"
                    d={repo.son_durum === 'basarili' ? '✓ başarılı' : (repo.son_durum === 'hata' || repo.son_durum.startsWith('hata') ? '⚠ hata' : repo.son_durum)}
                    renk={repo.son_durum === 'basarili' ? 'emerald' : (repo.son_durum.startsWith('hata') ? 'red' : 'slate')}
                  />
                </div>
                {logSon && (
                  <div className="mt-4 pt-3 border-t border-slate-200 dark:border-slate-700">
                    <div className="text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-1">Son log</div>
                    <pre className="text-xs bg-slate-900 text-slate-100 p-3 rounded-md overflow-auto max-h-60 font-mono whitespace-pre-wrap">{logSon}</pre>
                  </div>
                )}
              </div>
            </>
          )}
        </>
      )}

      <ConfirmDialog
        acik={klonOnay}
        baslik="İlk klonlama"
        mesaj={`Hedef dizin (${targetDir}) içeriği TAMAMEN silinip repodaki ${branch} branch'i klonlanacak. Devam edilsin mi?`}
        tehlikeli
        onayMetni="Evet, klonla"
        onOnay={klonla}
        onIptal={() => setKlonOnay(false)}
      />

      <ConfirmDialog
        acik={silinecek}
        baslik="Repo bağlantısını kaldır"
        mesaj="Panel'deki kayıt silinecek (klonlanmış dosyalar diskte kalır)."
        tehlikeli
        onayMetni="Evet, sil"
        onOnay={sil}
        onIptal={() => setSilinecek(false)}
      />
    </div>
  )
}

function Sat({ e, d, onCopy }: { e: string; d: string; onCopy?: (s: string) => void }) {
  return (
    <div className="flex items-center gap-3">
      <span className="text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 w-28">{e}</span>
      <code className="flex-1 text-xs bg-slate-50 dark:bg-slate-900 px-2 py-1 rounded font-mono break-all">{d}</code>
      {onCopy && <button onClick={() => onCopy(d)} className="text-xs px-2 py-1 bg-slate-100 dark:bg-slate-800 hover:bg-brand-100 dark:bg-brand-900/30 rounded">⧉</button>}
    </div>
  )
}

function Stat({ e, d, mono, renk }: { e: string; d: string; mono?: boolean; renk?: string }) {
  const c: Record<string, string> = {
    emerald: 'text-emerald-700 dark:text-emerald-300',
    red: 'text-red-700 dark:text-red-300',
    slate: 'text-slate-700 dark:text-slate-300',
  }
  return (
    <div>
      <div className="text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500">{e}</div>
      <div className={`mt-0.5 ${mono ? 'font-mono text-xs' : 'text-sm font-medium'} ${c[renk || ''] || 'text-slate-800 dark:text-slate-200'}`}>{d}</div>
    </div>
  )
}