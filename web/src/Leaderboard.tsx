import {useEffect,useState} from 'react';
import {api,type LeaderboardData,type LeaderboardEntry,type User} from './api';
import {Avatar} from './Avatar';

export function Leaderboard({user,githubEnabled,refreshKey}:{user:User|null|undefined;githubEnabled:boolean;refreshKey:number}) {
 const [board,setBoard]=useState<LeaderboardData>(),[loading,setLoading]=useState(true),[error,setError]=useState(false),[retry,setRetry]=useState(0);
 const viewer=user===undefined?undefined:user?.id??'guest';
 useEffect(()=>{
  if(viewer===undefined)return;
  const controller=new AbortController();setLoading(true);setError(false);
  void api<LeaderboardData>('/api/leaderboard',{signal:controller.signal}).then(data=>{if(!controller.signal.aborted)setBoard(data)}).catch(()=>{if(!controller.signal.aborted)setError(true)}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return()=>controller.abort();
 },[viewer,refreshKey,retry]);
 return <section className="leaderboard panel" aria-labelledby="leaderboard-heading" aria-busy={loading}>
  <div className="leaderboard-heading"><div><span className="eyebrow">LEARNING TOGETHER</span><h2 id="leaderboard-heading">Leaderboard</h2></div><span className="leaderboard-period">This week</span></div>
  <p className="leaderboard-intro">A little practice. A shared goal.</p>
  {error?<div className="leaderboard-message"><p role="status">The leaderboard is unavailable right now.</p><button className="text-button" onClick={()=>setRetry(r=>r+1)}>Retry leaderboard</button></div>:!board?<p className="leaderboard-message" role="status">Finding this week’s learners…</p>:<>
   {board.entries.length?<ol className="leaderboard-list">{board.entries.map(entry=><LeaderRow key={entry.login} entry={entry}/>)}</ol>:<div className="leaderboard-empty"><strong>A fresh week starts here.</strong><p>The first correct answer takes the lead.</p></div>}
   {board.you&&!board.entries.some(entry=>entry.isYou)&&<div className="leaderboard-own"><span className="eyebrow">YOUR PLACE</span><ol className="leaderboard-list"><LeaderRow entry={board.you}/></ol></div>}
   {user&&!board.you&&<p className="leaderboard-invitation">Your next correct answer starts your week.</p>}
   {user===null&&githubEnabled&&<a className="leaderboard-join" href="/auth/github">Sign in to join the leaderboard →</a>}
  </>}
  <p className="leaderboard-rules">1 point per question answered correctly, counted once a week. Ties share a rank. Resets Monday at 00:00 UTC.</p>
 </section>
}

function LeaderRow({entry}:{entry:LeaderboardEntry}) {
 return <li className={entry.isYou?'leaderboard-row is-you':'leaderboard-row'}>
  <span className={'leaderboard-rank '+(entry.rank<=3?'top-rank':'')} aria-label={'Rank '+entry.rank}>{entry.rank}</span>
  <Avatar login={entry.login}/>
  <span className="leaderboard-login" title={entry.login}>{entry.login}</span>
  {entry.isYou&&<span className="leaderboard-you">You</span>}
  <span className="leaderboard-score" aria-label={`${entry.score} ${entry.score===1?'point':'points'}`}><strong>{entry.score}</strong><span>pts</span></span>
 </li>
}
