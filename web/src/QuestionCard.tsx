import {useState} from 'react';
import {api,json,type Draft,type DraftEdit} from './api';

export type QuestionForm={kind:Draft['kind'];prompt:string;answers:string;options:string;explanation:string};
export const formFor=(d:Draft):QuestionForm=>({kind:d.kind,prompt:d.prompt,answers:d.answers.join('\n'),options:d.options.join('\n'),explanation:d.explanation});
export const editFor=(f:QuestionForm):DraftEdit=>({kind:f.kind,prompt:f.prompt,answers:f.answers.split('\n').map(s=>s.trim()).filter(Boolean),options:f.kind==='cloze'?[]:f.options.split('\n').map(s=>s.trim()).filter(Boolean),explanation:f.explanation});

export function QuestionCard({draft,form,onChange,selected,onSelect,disabled,onSave,materialTitle}: {
 draft:Draft;form:QuestionForm;onChange:(form:QuestionForm)=>void;selected?:boolean;onSelect?:(selected:boolean)=>void;
 disabled:boolean;onSave:(action:()=>Promise<void>)=>Promise<boolean>;materialTitle?:string;
}) {
 const [editing,setEditing]=useState(false),[confirmDelete,setConfirmDelete]=useState(false);
 const update=(change:Partial<QuestionForm>)=>onChange({...form,...change});
 const save=async()=>{await api('/api/exercises/'+draft.id,json('PATCH',editFor(form)))};
 const publish=async()=>{await api('/api/drafts/'+draft.id+'/status',json('POST',{status:'published'}))};
 async function perform(action:()=>Promise<void>){if(await onSave(action)){setEditing(false);setConfirmDelete(false)}}
 return <article className={'draft question-card '+draft.status}>
  <div className="step"><span className="eyebrow">{draft.kind==='cloze'?'FILL THE GAP':'MULTIPLE CHOICE'}</span><span className="status-tag">{draft.status}</span></div>
  {materialTitle&&<p className="question-origin">{materialTitle}</p>}
  {onSelect&&draft.status==='draft'&&<label className="checkbox"><input type="checkbox" aria-label={'Select question: '+draft.prompt} checked={selected??false} disabled={disabled} onChange={e=>onSelect(e.target.checked)}/>Select question</label>}
  {editing?<fieldset disabled={disabled}>
   <label>Question<textarea lang="ro" value={form.prompt} onChange={e=>update({prompt:e.target.value})} rows={3}/></label>
   <label>Exercise type<select value={form.kind} onChange={e=>update({kind:e.target.value as Draft['kind']})}><option value="cloze">Fill in the blank</option><option value="multiple_choice">Multiple choice</option></select></label>
   <div className="answer-grid"><label>Accepted answers<textarea lang="ro" value={form.answers} onChange={e=>update({answers:e.target.value})} rows={2}/><small>One per line. Use the correct Romanian form.</small></label>{form.kind==='multiple_choice'&&<label>Answer options<textarea lang="ro" value={form.options} onChange={e=>update({options:e.target.value})} rows={3}/><small>One per line, including the correct answer.</small></label>}</div>
   <label>Explanation<textarea value={form.explanation} onChange={e=>update({explanation:e.target.value})} rows={3}/></label>
  </fieldset>:<div className="question-preview-content">
   <h3 lang="ro">{draft.prompt}</h3>
   {draft.options.length>0&&<ul className="preview-options">{draft.options.map(option=><li key={option} lang="ro" className={draft.answers.some(a=>a.trim().toLocaleLowerCase()===option.trim().toLocaleLowerCase())?'correct-option':''}>{option}{draft.answers.some(a=>a.trim().toLocaleLowerCase()===option.trim().toLocaleLowerCase())&&<span aria-label="Correct answer"> ✓</span>}</li>)}</ul>}
   <p className="preview-answer"><span>Answer</span> <strong lang="ro">{draft.answers.join(' / ')}</strong></p><p className="preview-explanation">{draft.explanation}</p>
  </div>}
  {draft.sourceQuote&&<details className="question-source"><summary>Lesson evidence</summary><blockquote lang="ro">{draft.sourceQuote}<cite>Original source · line {draft.sourceLine}</cite></blockquote></details>}
  <div className="draft-actions">
   {editing?<><button disabled={disabled} onClick={()=>void perform(save)}>Save changes</button><button className="secondary" disabled={disabled} onClick={()=>{onChange(formFor(draft));setEditing(false)}}>Cancel edit</button></>:<button className="secondary" disabled={disabled} onClick={()=>setEditing(true)}>Edit question</button>}
   {draft.status==='draft'&&<button disabled={disabled} onClick={()=>void perform(async()=>{if(editing)await save();await publish()})}>{editing?'Save & publish':'Publish question'}</button>}
   {!confirmDelete&&<button className="text-button delete-question" disabled={disabled} onClick={()=>setConfirmDelete(true)}>Delete question</button>}
  </div>
  {confirmDelete&&<div className="delete-confirmation"><p>Remove this question from the library and practice? Saved answers will remain.</p><div className="draft-actions"><button className="danger" disabled={disabled} onClick={()=>void perform(async()=>{await api('/api/exercises/'+draft.id,{method:'DELETE'})})}>Confirm deletion</button><button className="secondary" disabled={disabled} onClick={()=>setConfirmDelete(false)}>Cancel</button></div></div>}
 </article>
}
