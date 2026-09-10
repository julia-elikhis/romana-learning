import {useEffect,useState} from 'react';
import {api,json,type ReportPage} from './api';
import {QuestionCard,formFor,type QuestionForm} from './QuestionCard';

export function Reports(){
 const [status,setStatus]=useState('open'),[page,setPage]=useState(1),[revision,setRevision]=useState(0);
 const [data,setData]=useState<ReportPage>(),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('');
 const [edits,setEdits]=useState<Record<string,QuestionForm>>({});
 useEffect(()=>{
  const controller=new AbortController();setLoading(true);setError('');
  void api<ReportPage>('/api/admin/reports?'+new URLSearchParams({status,page:String(page)}),{signal:controller.signal}).then(result=>{
   if(controller.signal.aborted)return;
   if(page>1&&!result.reports.length){setPage(Math.max(1,Math.ceil(result.total/result.pageSize)));return}
   setData(result);setEdits({});
  }).catch(e=>{if(!controller.signal.aborted)setError(e.message)}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return()=>controller.abort();
 },[status,page,revision]);
 async function save(action:()=>Promise<void>,message='Question updated.'){
  if(busy)return false;setBusy(true);setError('');setNotice('');
  try{await action();setNotice(message);setRevision(r=>r+1);return true}
  catch(e){setError((e as Error).message);return false}finally{setBusy(false)}
 }
 return <main className="management-layout">
  <div className="library-heading"><div><span className="eyebrow">QUESTION REPORTS</span><h1>A second look.</h1><p>Review what learners noticed, correct the question, and resolve the report.</p></div><label>Report status<select value={status} disabled={busy} onChange={e=>{setStatus(e.target.value);setPage(1);setNotice('')}}><option value="open">Open</option><option value="resolved">Resolved</option></select></label></div>
  {error&&<p role="alert" className="error banner">{error}<button className="text-button" onClick={()=>setRevision(r=>r+1)}>Retry reports</button></p>}
  {notice&&<p role="status" className="feedback">{notice}</p>}
  <p className="results-count" role="status">{loading?'Loading reports…':data?`${data.total} ${status} ${data.total===1?'report':'reports'}`:''}</p>
  <div className="report-collection" aria-busy={loading}>
   {!loading&&data?.reports.length===0&&<div className="panel"><h2>{status==='open'?'All caught up.':'No resolved reports yet.'}</h2><p>{status==='open'?'New question reports will appear here.':'Reviewed reports will be kept here.'}</p></div>}
   {data?.reports.map(report=><section key={report.id} className="report-card panel" aria-label={'Report: '+report.prompt}>
    <div className="report-meta"><span>{report.reporter||'Anonymous learner'}</span><time dateTime={report.createdAt}>{new Date(report.createdAt).toLocaleDateString()}</time><span className="status-tag">{report.status}</span></div>
    <p className="report-note">{report.note||'This question was flagged for review without a note.'}</p>
    {report.question?<>
     {report.prompt!==report.question.prompt&&<details><summary>Question when reported</summary><p>{report.prompt}</p></details>}
     <QuestionCard draft={report.question} form={edits[report.id]??formFor(report.question)} onChange={form=>setEdits(prev=>({...prev,[report.id]:form}))} disabled={loading||busy} onSave={save}/>
    </>:<div className="removed-question"><p>{report.prompt}</p><p className="note">This question has been removed from practice.</p></div>}
    <button className={report.status==='open'?'resolve-report':'secondary resolve-report'} disabled={busy||loading} onClick={()=>void save(async()=>{await api('/api/admin/reports/'+report.id,json('PATCH',{status:report.status==='open'?'resolved':'open'}))},report.status==='open'?'Report resolved.':'Report reopened.')}>{report.status==='open'?'Mark resolved':'Reopen report'}</button>
   </section>)}
  </div>
  {data&&data.total>data.pageSize&&<nav className="pagination" aria-label="Report pages"><button className="secondary" disabled={loading||busy||page===1} onClick={()=>setPage(p=>p-1)}>Previous</button><span>Page {page} of {Math.ceil(data.total/data.pageSize)}</span><button className="secondary" disabled={loading||busy||page*data.pageSize>=data.total} onClick={()=>setPage(p=>p+1)}>Next</button></nav>}
 </main>
}
