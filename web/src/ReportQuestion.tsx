import {useState} from 'react';
import {api,json} from './api';

export function ReportQuestion({exerciseId,disabled}:{exerciseId:string;disabled:boolean}) {
 const [open,setOpen]=useState(false),[note,setNote]=useState(''),[sending,setSending]=useState(false),[sent,setSent]=useState(false),[error,setError]=useState('');
 const [id,setID]=useState(()=>crypto.randomUUID());
 async function send(){
  if(sending||disabled)return;setSending(true);setError('');
  try{await api('/api/exercises/'+encodeURIComponent(exerciseId)+'/reports',json('POST',{id,note}));setSent(true);setOpen(false)}
  catch(e){setError((e as Error).message)}finally{setSending(false)}
 }
 if(sent)return <p className="report-sent" role="status">Report sent. An admin will review this question.</p>;
 return <div className="report-question">
  {!open?<button className="text-button report-trigger" disabled={disabled} onClick={()=>setOpen(true)}><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M5 21V4c5-5 9 5 15 0v11c-6 5-10-5-15 0"/></svg>Report question</button>
  :<form className="report-form" aria-label="Report question" onSubmit={e=>{e.preventDefault();void send()}}>
   <label>What seems wrong? <span>(optional)</span><textarea rows={3} maxLength={1000} value={note} disabled={sending||disabled} placeholder="A typo, an unclear question, or an answer that seems incorrect…" onChange={e=>{setNote(e.target.value);setID(crypto.randomUUID())}}/></label>
   <div className="report-actions"><button type="submit" disabled={sending||disabled}>{sending?'Sending…':'Send report'}</button><button type="button" className="text-button" disabled={sending} onClick={()=>{setOpen(false);setError('')}}>Cancel report</button></div>
   {error&&<p role="alert" className="error">{error}</p>}
  </form>}
 </div>
}
