import { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';

type Exercise={id:string;prompt:string;options:string[]};
type Progress={attempts:number;correct:number;practiced:number};
type Result={correct:boolean;answer:string;explanation:string};
async function api<T>(path:string,init?:RequestInit):Promise<T>{
 const response=await fetch(path,init);const data=await response.json();
 if(!response.ok)throw new Error(data.error||'Unable to connect');return data;
}
function App(){
 const [exercises,setExercises]=useState<Exercise[]>([]),[progress,setProgress]=useState<Progress>();
 const [active,setActive]=useState(false),[index,setIndex]=useState(0),[choice,setChoice]=useState('');
 const [result,setResult]=useState<Result>(),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [attemptID,setAttemptID]=useState(()=>crypto.randomUUID());
 const refresh=()=>api<Progress>('/api/progress').then(setProgress);
 async function load(){setError('');try{const [items,p]=await Promise.all([api<Exercise[]>('/api/exercises'),api<Progress>('/api/progress')]);setExercises(items);setProgress(p)}catch(e){setError((e as Error).message)}}
 useEffect(()=>{void load()},[]);
 async function submit(){if(!choice||busy)return;setBusy(true);setError('');try{
  const r=await api<Result>('/api/attempts',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:attemptID,exerciseId:exercises[index].id,answer:choice})});
  setResult(r);await refresh();
 }catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 function next(){setIndex(index+1);setChoice('');setResult(undefined);setAttemptID(crypto.randomUUID());setError('')}
 function start(){setIndex(0);setChoice('');setResult(undefined);setAttemptID(crypto.randomUUID());setActive(true)}
 return <><header><a className="brand" href="/">puțin<span>ROMANIAN, A LITTLE EVERY DAY</span></a><span className="pill">Your road to B1 · December</span></header>
 <main><aside><span className="eyebrow">YOUR PRACTICE SPACE</span><h1>A little today.<br/><em>More confidence tomorrow.</em></h1><p>Pick up the words you know. Put them to work. Leave with one small win.</p><div className="stats"><div><strong>{progress?.attempts??'—'}</strong><span>saved answers</span></div><div><strong>{progress?.practiced??'—'} / 5</strong><span>items answered correctly</span></div></div><p className="note">Local development · one learner<br/>Progress is saved in your Postgres database.</p></aside>
 <section className="card" aria-label="Practice mission">
 {!active?<><span className="eyebrow">MISSION 01 · AT HOME</span><div className="illustration" aria-hidden="true">⌂</div><h2>Make yourself at home</h2><p>Practice familiar words, plurals, and useful verbs in five quick questions.</p><div className="tags"><span>5 questions</span><span>Foundation practice</span></div><button disabled={!exercises.length} onClick={start}>Start a little practice <span>→</span></button><p className="note">This first demo contains one short mission.<br/>Longer sessions and exam modes are coming next.</p></>:
 index>=exercises.length?<><span className="eyebrow">A SMALL WIN</span><div className="illustration">✓</div><h2>You showed up.</h2><p>Your answers are saved. Come back to build on them, one small step at a time.</p><button onClick={()=>setActive(false)}>Back to today →</button></>:
 <><div className="step"><span className="eyebrow">AT HOME</span><span>{index+1} / {exercises.length}</span></div><progress value={index} max={exercises.length}/><h2>{exercises[index].prompt}</h2><div className="options">{exercises[index].options.map(option=><button key={option} disabled={busy||!!result} className={choice===option?'option selected':'option'} onClick={()=>{setChoice(option);setAttemptID(crypto.randomUUID())}}>{option}</button>)}</div>
 {result?<div className="feedback" role="status"><strong>{result.correct?'Nicely done.':'A useful one to remember.'}</strong><p>{result.explanation}</p><span>Saved to Postgres</span></div>:<p className="note">Give it a try. Mistakes are part of practice.</p>}
 {result?<button onClick={next}>{index===exercises.length-1?'Finish mission':'Next question'} →</button>:<button disabled={!choice||busy} onClick={()=>void submit()}>{busy?'Saving…':'Check answer'}</button>}</>}
 {error&&<div role="alert" className="error">{error} {!exercises.length&&<button onClick={()=>void load()}>Retry connection</button>}</div>}
 </section></main><footer>Small steps. Real Romanian. <span>Built around your learning.</span></footer></>;
}
createRoot(document.getElementById('root')!).render(<App/>);
