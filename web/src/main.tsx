import {useEffect,useRef,useState} from 'react';
import {createRoot} from 'react-dom/client';
import './style.css';
import {api,json,type Exercise,type Result,type Material,type AuthSession} from './api';
import {Library} from './Library';
import {History} from './History';
import {Questions} from './Questions';
import {Users} from './Users';
import {Leaderboard} from './Leaderboard';
import {Avatar,SignOutIcon} from './Avatar';
import {ReportQuestion} from './ReportQuestion';
import {Reports} from './Reports';

function App(){
 const [page,setPage]=useState<'practice'|'library'|'history'|'questions'|'users'|'reports'>('practice'),[lesson,setLesson]=useState<Material>();
 const [session,setSession]=useState<AuthSession>(),[exercise,setExercise]=useState<Exercise|null>(null);
 const [choice,setChoice]=useState(''),[result,setResult]=useState<Result>(),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [attemptID,setAttemptID]=useState(()=>crypto.randomUUID());
 const [loading,setLoading]=useState(true),[leaderboardVersion,setLeaderboardVersion]=useState(0);
 const loadVersion=useRef(0),loadController=useRef<AbortController|null>(null),guestAnswers=useRef<string[]>([]);

 async function load(material?:Material,currentId=''){
  const version=++loadVersion.current;
  loadController.current?.abort();const controller=new AbortController();loadController.current=controller;
  setLoading(true);setError('');setPage('practice');setLesson(material);
  const params=new URLSearchParams();
  if(material)params.set('materialId',material.id);
  if(currentId)params.set('currentId',currentId);
  if(!session?.user)for(const id of guestAnswers.current)params.append('answeredId',id);
  try{
   const [item,s]=await Promise.all([
    api<Exercise|null>('/api/practice/question'+(params.size?'?'+params:''),{signal:controller.signal}),
    api<AuthSession>('/api/auth/session',{signal:controller.signal}).then(s=>{if(version===loadVersion.current)setSession(s);return s}),
   ]);
   if(version!==loadVersion.current)return;
   setExercise(item);setSession(s);setChoice('');setResult(undefined);setAttemptID(crypto.randomUUID());
   if(new URLSearchParams(location.search).has('authError')){
    setError('GitHub sign-in could not be completed. Please try again.');history.replaceState(null,'',location.pathname);
   }
  }catch(e){if(version===loadVersion.current&&!controller.signal.aborted)setError((e as Error).message)}
  finally{if(version===loadVersion.current)setLoading(false)}
 }
 useEffect(()=>{void load();return()=>{loadVersion.current++;loadController.current?.abort()}},[]);
 async function submit(){
  if(!exercise||!choice.trim()||busy||loading||result)return;
  setBusy(true);setError('');
  try{
   const r=await api<Result>('/api/attempts',json('POST',{id:attemptID,exerciseId:exercise.id,answer:choice}));
   setResult(r);
   if(r.saved)setLeaderboardVersion(v=>v+1);
   else guestAnswers.current=[...new Set([...guestAnswers.current,exercise.id])].slice(-200);
  }catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 async function signOut(){
  setBusy(true);
  try{await api('/api/auth/logout',{method:'POST'});guestAnswers.current=[];setSession(s=>s?{...s,user:null}:s);await load()}
  catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 const signIn=session?.githubEnabled?<a className="sign-in" href="/auth/github">Sign in with GitHub</a>:<button className="nav-button" disabled>Sign in with GitHub</button>;
 return <>
  <header>
   <a className="brand" href="/">puțin<span>ROMANIAN, A LITTLE EVERY DAY</span></a>
   <nav aria-label="Main navigation">
    <button disabled={busy} className={page==='practice'?'nav-button current':'nav-button'} onClick={()=>void load()}>Practice</button>
    {session?.user?.isAdmin&&<>
     <button disabled={busy} className={page==='library'?'nav-button current':'nav-button'} onClick={()=>setPage('library')}>Course library</button>
     <button disabled={busy} className={page==='questions'?'nav-button current':'nav-button'} onClick={()=>setPage('questions')}>Questions</button>
     <button disabled={busy} className={page==='reports'?'nav-button current':'nav-button'} onClick={()=>setPage('reports')}>Reports</button>
     <button disabled={busy} className={page==='users'?'nav-button current':'nav-button'} onClick={()=>setPage('users')}>Users</button>
    </>}
    {session?.user?<>
     <button disabled={busy} className={page==='history'?'nav-button current':'nav-button'} onClick={()=>setPage('history')}>History</button>
     <div className="account-chip" aria-label={'Signed in as '+session.user.login}>
      <Avatar login={session.user.login}/><span className="account-name" title={session.user.login}>{session.user.login}</span>
      <button className="sign-out" aria-label="Sign out" title="Sign out" disabled={busy} onClick={()=>void signOut()}><SignOutIcon/></button>
     </div>
    </>:signIn}
   </nav>
  </header>
  {page!=='practice'&&!session?.user?<main className="auth-layout"><section className="card"><span className="eyebrow">YOUR LEARNING ACCOUNT</span><h1>Make room for your lessons.</h1><p>Sign in to keep your own practice history.</p>{signIn}{!session?<p>Checking sign-in…</p>:!session.githubEnabled&&<p className="note">Sign-in is currently unavailable. You can still practice published questions.</p>}</section></main>
  :page!=='practice'&&page!=='history'&&!session?.user?.isAdmin?<main className="auth-layout"><section className="card"><h1>Administrator access required.</h1><p>You can keep practising and viewing your history.</p><button onClick={()=>void load()}>Back to practice</button></section></main>
  :page==='questions'?<Questions/>
  :page==='reports'?<Reports/>
  :page==='users'&&session?.user?<Users currentUser={session.user}/>
  :page==='library'?<Library onPractice={m=>void load(m)}/>
  :page==='history'?<History/>
  :<main className="practice-layout">
   <div className="practice-intro"><span className="eyebrow">YOUR PRACTICE SPACE</span><h1>A little today.<br/><em>More confidence tomorrow.</em></h1></div>
   <section className="card practice-card" aria-label="Practice mission" aria-busy={loading}>
    {loading?<div className="question-loading" role="status"><span className="generation-spinner" aria-hidden="true"/><p>Finding a question…</p></div>
    :exercise?<>
     <span className="eyebrow">{lesson?lesson.title:'MIXED PRACTICE'}</span>
     <h2 className="exercise-prompt">{exercise.prompt}</h2>
     {exercise.kind==='cloze'?<div className="typed-answer"><label>Your answer<input lang="ro" autoComplete="off" autoCapitalize="none" spellCheck={false} maxLength={200} value={choice} disabled={busy||!!result} onChange={e=>{setChoice(e.target.value);setAttemptID(crypto.randomUUID())}} onKeyDown={e=>{if(e.key==='Enter'&&!result)void submit()}}/></label><div className="diacritics" aria-label="Romanian letters">{['ă','â','î','ș','ț'].map(c=><button key={c} className="secondary" disabled={busy||!!result} onClick={()=>{setChoice(choice+c);setAttemptID(crypto.randomUUID())}}>{c}</button>)}</div></div>
     :<div className="options">{exercise.options.map(option=><button key={option} disabled={busy||!!result} className={choice===option?'option selected':'option'} onClick={()=>{setChoice(option);setAttemptID(crypto.randomUUID())}}>{option}</button>)}</div>}
     {result?<div className={'feedback '+(result.correct?'feedback-correct':'feedback-incorrect')} role="status"><strong>{result.correct?'Nicely done.':'Not quite. A useful one to remember.'}</strong><p>{!result.correct&&<>Answer: <strong lang="ro">{result.answer}</strong><br/></>}{result.explanation}</p><span>{result.saved?'Saved to your history':'Anonymous practice · answer not saved'}</span></div>
     :<p className="note">{session?.user?'Give it a try. Mistakes are part of practice.':'Anonymous practice · answers are not saved.'}</p>}
     {result?<button disabled={busy} onClick={()=>void load(lesson,exercise.id)}>Next question <span aria-hidden="true">→</span></button>
     :<><button disabled={!choice.trim()||busy} onClick={()=>void submit()}>{busy?'Checking…':'Check answer'}</button><button className="secondary shuffle" disabled={busy} onClick={()=>void load(lesson,exercise.id)}>Shuffle question <span aria-hidden="true">↻</span></button></>}
     {lesson&&<button className="text-button" disabled={busy} onClick={()=>void load()}>Back to mixed practice</button>}
     <ReportQuestion key={exercise.id} exerciseId={exercise.id} disabled={busy}/>
    </>:!error?<div className="empty-practice"><span className="eyebrow">YOUR PRACTICE QUESTIONS</span><h2>No practice questions yet.</h2><p>{session?.user?.isAdmin?'Publish questions from your course library to start practising.':'Published questions will appear here when they are ready.'}</p>{session?.user?.isAdmin&&<button onClick={()=>setPage('library')}>Open course library <span aria-hidden="true">→</span></button>}</div>:null}
    {error&&<div role="alert" className="error">{error}<button className="text-button" disabled={loading||busy} onClick={()=>void load(lesson,exercise?.id)}>Retry connection</button></div>}
   </section>
   <Leaderboard user={session?.user} githubEnabled={!!session?.githubEnabled} refreshKey={leaderboardVersion}/>
  </main>}
  <footer>Small steps. Real Romanian. <span>Built around your learning.</span></footer>
 </>;
}
createRoot(document.getElementById('root')!).render(<App/>);
