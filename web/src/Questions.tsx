import {useEffect,useRef,useState} from 'react';
import {api,type QuestionPage} from './api';
import {QuestionCard,formFor,type QuestionForm} from './QuestionCard';

export function Questions(){
 const [query,setQuery]=useState(''),[status,setStatus]=useState(''),[page,setPage]=useState(1);
 const [data,setData]=useState<QuestionPage>(),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false);
 const [error,setError]=useState(''),[notice,setNotice]=useState(''),[revision,setRevision]=useState(0);
 const [edits,setEdits]=useState<Record<string,QuestionForm>>({});
 const requestVersion=useRef(0);
 useEffect(()=>{
  const version=++requestVersion.current,controller=new AbortController();
  setLoading(true);setError('');
  const timer=setTimeout(()=>{
   const params=new URLSearchParams({q:query,status,page:String(page)});
   void api<QuestionPage>('/api/admin/questions?'+params,{signal:controller.signal}).then(result=>{
    if(version!==requestVersion.current)return;
    if(page>1&&!result.questions.length){setPage(Math.max(1,Math.ceil(result.total/result.pageSize)));return}
    setData(result);setEdits({});
   }).catch(e=>{if(!controller.signal.aborted&&version===requestVersion.current)setError(e.message)}).finally(()=>{if(!controller.signal.aborted&&version===requestVersion.current)setLoading(false)});
  },250);
  return()=>{clearTimeout(timer);controller.abort()};
 },[query,status,page,revision]);
 async function save(action:()=>Promise<void>){
  if(busy)return false;setBusy(true);setError('');setNotice('');
  try{await action();setNotice('Question updated.');setRevision(r=>r+1);return true}catch(e){setError((e as Error).message);return false}finally{setBusy(false)}
 }
 return <main className="management-layout">
  <div className="library-heading"><div><span className="eyebrow">YOUR QUESTION COLLECTION</span><h1>Find it. Fine-tune it.</h1><p>Browse every question, check the answers, and make a quick correction.</p></div></div>
  <section className="question-toolbar panel" aria-label="Search questions"><label>Search questions<input type="search" placeholder="Words, answers, explanations, or lesson titles…" value={query} maxLength={300} disabled={busy} onChange={e=>{setQuery(e.target.value);setPage(1);setNotice('')}}/></label><label>Status<select value={status} disabled={busy} onChange={e=>{setStatus(e.target.value);setPage(1)}}><option value="">All questions</option><option value="published">Published</option><option value="draft">Drafts</option><option value="rejected">Rejected</option></select></label></section>
  {error&&<p role="alert" className="error banner">{error} <button className="text-button" onClick={()=>setRevision(r=>r+1)}>Retry</button></p>}
  {notice&&<p role="status" className="feedback">{notice}</p>}
  <p role="status" className="results-count">{loading?'Finding questions…':data?`${data.total} ${data.total===1?'question':'questions'}${query?' matching your search':''}`:''}</p>
  <div className="question-collection" aria-busy={loading}>
   {!loading&&data?.questions.length===0&&<div className="panel empty-results"><h2>No questions found.</h2><p>Try a different search or status.</p></div>}
   {data?.questions.map(d=><QuestionCard key={d.id} draft={d} materialTitle={d.materialTitle} form={edits[d.id]??formFor(d)} onChange={form=>setEdits(prev=>({...prev,[d.id]:form}))} disabled={busy||loading} onSave={save}/>)}
  </div>
  {data&&data.total>data.pageSize&&<nav className="pagination" aria-label="Question pages"><button className="secondary" disabled={loading||busy||page===1} onClick={()=>setPage(p=>p-1)}>Previous</button><span>Page {page} of {Math.ceil(data.total/data.pageSize)}</span><button className="secondary" disabled={loading||busy||page*data.pageSize>=data.total} onClick={()=>setPage(p=>p+1)}>Next</button></nav>}
 </main>
}
