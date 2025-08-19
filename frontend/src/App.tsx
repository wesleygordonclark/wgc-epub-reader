import React, { useEffect, useMemo, useState } from 'react'

const API = import.meta.env.VITE_API || 'http://localhost:8080'

type Book = { id: string; title: string; author: string }

type SpineItem = { idref: string; href: string; mediaType: string; title: string }

type TOC = { items: { href: string; text: string }[] }

export default function App() {
  const [books, setBooks] = useState<Book[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [spine, setSpine] = useState<SpineItem[]>([])
  const [toc, setToc] = useState<TOC | null>(null)
  const [chapterUrl, setChapterUrl] = useState<string | null>(null)
  const [theme, setTheme] = useState<'light' | 'dark'>('light')
  const [fontSize, setFontSize] = useState<number>(18)

  // load library
  useEffect(() => {
    fetch(`${API}/api/books`).then(r => r.json()).then(setBooks).catch(()=>{})
  }, [])

  // when selecting a book, fetch spine and toc
  useEffect(() => {
    if (!activeId) return
    fetch(`${API}/api/books/${activeId}/spine`).then(r=>r.json()).then(setSpine)
    fetch(`${API}/api/books/${activeId}/toc`).then(r=>r.json()).then(setToc)
  }, [activeId])

  const openFirst = () => {
    if (!activeId || spine.length === 0) return
    const href = spine[0].href
    setChapterUrl(`${API}/api/books/${activeId}/file/${href}`)
  }

  const onUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files?.length) return
    const f = e.target.files[0]
    const fd = new FormData()
    fd.append('file', f)
    await fetch(`${API}/api/upload`, { method: 'POST', body: fd })
    const list = await (await fetch(`${API}/api/books`)).json()
    setBooks(list)
  }

  useEffect(()=>{ if(activeId) openFirst() }, [activeId, spine.length])

  const bodyClass = useMemo(() => theme === 'dark' ? 'app dark' : 'app', [theme])

  return (
    <div className={bodyClass}>
      <aside className="sidebar">
        <h2>Library</h2>
        <div style={{marginBottom:8}}>
          <label className="btn">
            Upload EPUB
            <input type="file" accept=".epub" onChange={onUpload} style={{display:'none'}} />
          </label>
        </div>
        {books.map(b => (
          <div key={b.id} className="book-card">
            <div style={{fontWeight:600}}>{b.title || '(untitled)'}</div>
            <div style={{opacity:.7, fontSize:12}}>{b.author}</div>
            <div style={{marginTop:6}}>
              <button className="btn" onClick={()=>setActiveId(b.id)}>Read</button>
            </div>
          </div>
        ))}
        {activeId && toc && toc.items.length > 0 && (
          <div style={{marginTop:16}}>
            <h3>Table of Contents</h3>
            <div className="toc">
              {toc.items.map((i, idx) => (
                <a key={idx} href="#" onClick={(e)=>{e.preventDefault(); setChapterUrl(`${API}/api/books/${activeId}/file/${i.href.startsWith('#') ? spine[0]?.href + i.href : i.href}`)}}>
                  {i.text}
                </a>
              ))}
            </div>
          </div>
        )}
      </aside>
      <main className="content">
        <div className="toolbar">
          <select className="select" value={theme} onChange={e=>setTheme(e.target.value as any)}>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
          <label style={{display:'flex', alignItems:'center', gap:6}}>
            Font size
            <input type="range" min={14} max={26} value={fontSize} onChange={e=>setFontSize(parseInt(e.target.value))} />
          </label>
          {activeId && chapterUrl && (
            <>
              <button className="btn" onClick={()=>openFirst()}>First</button>
              <button className="btn" onClick={()=>{
                const idx = spine.findIndex(s => `${API}/api/books/${activeId}/file/${s.href}` === chapterUrl)
                if (idx > 0) setChapterUrl(`${API}/api/books/${activeId}/file/${spine[idx-1].href}`)
              }}>Prev</button>
              <button className="btn" onClick={()=>{
                const idx = spine.findIndex(s => `${API}/api/books/${activeId}/file/${s.href}` === chapterUrl)
                if (idx >= 0 && idx < spine.length-1) setChapterUrl(`${API}/api/books/${activeId}/file/${spine[idx+1].href}`)
              }}>Next</button>
            </>
          )}
        </div>
        <div className="reader" style={{fontSize}}>
          {!activeId && <div className="page">Select a book to start reading.</div>}
          {activeId && !chapterUrl && <div className="page">Loading…</div>}
          {activeId && chapterUrl && (
            <iframe title="chapter" src={chapterUrl} style={{width:'100%', height:'100%', border:'0', background:'transparent'}} />
          )}
        </div>
      </main>
    </div>
  )
}