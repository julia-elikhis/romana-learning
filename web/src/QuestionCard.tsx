import {useState} from 'react';
import {api,json,formatNames,type Draft,type DraftEdit} from './api';

export type QuestionForm={kind:Draft['kind'];prompt:string;answers:string;options:string;explanation:string;skill:NonNullable<Draft['skill']>;target:string;difficulty:NonNullable<Draft['difficulty']>};
export const formFor=(d:Draft):QuestionForm=>({kind:d.kind,prompt:d.prompt,answers:d.answers.join('\n'),options:d.options.join('\n'),explanation:d.explanation,skill:d.skill??'',target:d.target??'',difficulty:d.difficulty??''});
export const editFor=(f:QuestionForm):DraftEdit=>({kind:f.kind,prompt:f.prompt,answers:f.answers.split('\n').map(s=>s.trim()).filter(Boolean),options:f.kind==='cloze'?[]:f.options.split('\n').map(s=>s.trim()).filter(Boolean),explanation:f.explanation,skill:f.skill,target:f.target,difficulty:f.difficulty});

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
  <div className="step"><span className="eyebrow">{formatNames[draft.kind].toUpperCase()}</span><span className="status-tag">{draft.status}</span></div>
  {materialTitle&&<p className="question-origin">{materialTitle}</p>}
  {onSelect&&draft.status==='draft'&&<label className="checkbox"><input type="checkbox" aria-label={'Select question: '+draft.prompt} checked={selected??false} disabled={disabled} onChange={e=>onSelect(e.target.checked)}/>Select question</label>}
  {editing?<fieldset disabled={disabled}>
   <label>Question<textarea lang="ro" value={form.prompt} onChange={e=>update({prompt:e.target.value})} rows={3}/></label>
   <label>Exercise type<select value={form.kind} onChange={e=>update({kind:e.target.value as Draft['kind']})}><option value="cloze">Fill in the blank</option><option value="multiple_choice">Multiple choice</option><option value="multi_select">Select all correct answers</option></select></label>
   <div className="answer-grid"><label>Accepted answers<textarea lang="ro" value={form.answers} onChange={e=>update({answers:e.target.value})} rows={2}/><small>{form.kind==='multi_select'?'List every correct option, one per line. All must be selected. At least two correct and one incorrect option are required.':'One per line. Use the correct Romanian form.'}</small></label>{form.kind!=='cloze'&&<label>Answer options<textarea lang="ro" value={form.options} onChange={e=>update({options:e.target.value})} rows={3}/><small>One per line, including the correct answer.</small></label>}</div>
   <details className="learning-focus"><summary>Learning focus</summary><label>Skill<select value={form.skill} onChange={e=>update({skill:e.target.value as QuestionForm['skill']})}><option value="">Unspecified</option><option value="grammar">Grammar</option><option value="vocabulary">Vocabulary</option><option value="communication">Communication</option><option value="reading">Reading</option></select></label><label>Learning target<input value={form.target} maxLength={160} onChange={e=>update({target:e.target.value})}/></label><label>Difficulty<select value={form.difficulty} onChange={e=>update({difficulty:e.target.value as QuestionForm['difficulty']})}><option value="">Unspecified</option><option value="easy">Easy</option><option value="medium">Medium</option><option value="hard">Hard</option></select></label></details>
   <label>Explanation<textarea value={form.explanation} onChange={e=>update({explanation:e.target.value})} rows={3}/></label>
  </fieldset>:<div className="question-preview-content">
   <h3 lang="ro">{draft.prompt}</h3>
   {draft.options.length>0&&<ul className="preview-options">{draft.options.map(option=><li key={option} lang="ro" className={draft.answers.some(a=>a.trim().toLocaleLowerCase()===option.trim().toLocaleLowerCase())?'correct-option':''}>{option}{draft.answers.some(a=>a.trim().toLocaleLowerCase()===option.trim().toLocaleLowerCase())&&<span aria-label="Correct answer"> ✓</span>}</li>)}</ul>}
   <p className="preview-answer"><span>Answer</span> <strong lang="ro">{draft.answers.join(draft.kind==='multi_select'?' + ':' / ')}</strong></p><p className="preview-explanation">{draft.explanation}</p>{draft.target&&<p className="question-focus">{draft.skill} · {draft.target} · {draft.difficulty}</p>}
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
