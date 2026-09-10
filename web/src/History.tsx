import {useEffect,useState} from 'react';
import {api,type HistoryItem} from './api';

export function History(){
 const [items,setItems]=useState<HistoryItem[]>(),[error,setError]=useState('');
 useEffect(()=>{void api<HistoryItem[]>('/api/history').then(setItems).catch(e=>setError(e.message))},[]);
 return <main className="history-layout"><span className="eyebrow">YOUR LEARNING HISTORY</span><h1>Your practice, remembered.</h1><p>Your most recent 100 answers. Only you can see this history.</p>
 {error&&<p role="alert" className="error">{error}</p>}
 {!items&&!error&&<p>Loading your answers…</p>}
 {items?.length===0&&<section className="panel"><h2>A fresh start.</h2><p>Answer a published question while signed in and it will appear here.</p></section>}
 {items?.map(item=><article className="panel history-item" key={item.id}><div className="step"><span className="status-tag">{item.correct?'Correct':'Keep practising'}</span><time dateTime={item.createdAt}>{new Date(item.createdAt).toLocaleString()}</time></div><h2 lang="ro">{item.prompt||'Earlier practice question'}</h2>{item.selections?.length?<div className="history-selections"><p>Your selections:</p><ul>{item.selections.map(value=><li key={value} lang="ro">{value}</li>)}</ul>{!item.correct&&<><p>Correct selections:</p><ul>{item.correctSelections?.map(value=><li key={value} lang="ro">{value}</li>)}</ul></>}</div>:<p>Your answer: <strong lang="ro">{item.answer}</strong>{!item.correct&&<><br/>Correct answer: <strong lang="ro">{item.correctAnswer}</strong></>}</p>}<p>{item.explanation}</p></article>)}
 </main>
}
