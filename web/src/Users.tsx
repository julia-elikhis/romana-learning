import {useEffect,useState} from 'react';
import {api,json,type User} from './api';

export function Users({currentUser}:{currentUser:User}){
 const [users,setUsers]=useState<User[]>([]),[query,setQuery]=useState(''),[revision,setRevision]=useState(0);
 const [loading,setLoading]=useState(true),[busy,setBusy]=useState(''),[error,setError]=useState(''),[notice,setNotice]=useState('');
 useEffect(()=>{
  const controller=new AbortController();setLoading(true);setError('');
  const timer=setTimeout(()=>{void api<User[]>('/api/admin/users?q='+encodeURIComponent(query),{signal:controller.signal}).then(data=>{if(!controller.signal.aborted)setUsers(data)}).catch(e=>{if(!controller.signal.aborted)setError(e.message)}).finally(()=>{if(!controller.signal.aborted)setLoading(false)})},200);
  return()=>{clearTimeout(timer);controller.abort()};
 },[query,revision]);
 async function changeRole(user:User){
  if(busy)return;setBusy(user.id);setError('');setNotice('');
  try{await api('/api/admin/users/'+user.id,json('PATCH',{isAdmin:!user.isAdmin}));setNotice(`${user.login} is now ${user.isAdmin?'a learner':'an admin'}.`);setRevision(r=>r+1)}catch(e){setError((e as Error).message)}finally{setBusy('')}
 }
 return <main className="management-layout"><div className="library-heading"><div><span className="eyebrow">PEOPLE & ACCESS</span><h1>A little help with the lessons.</h1><p>Admins can add course materials, generate and edit questions, and manage access.</p></div></div>
  <section className="panel"><label>Search users<input type="search" maxLength={200} placeholder="GitHub username or name" value={query} disabled={!!busy} onChange={e=>setQuery(e.target.value)}/></label><p className="note">Users appear after their first GitHub sign-in. Showing up to 100 matches.</p>
  {error&&<p role="alert" className="error">{error}</p>}{notice&&<p role="status" className="feedback">{notice}</p>}
  {loading&&<p role="status">Finding users…</p>}
  {!loading&&!users.length&&<p>No users match this search.</p>}
  <ul className="user-list">{users.map(user=><li key={user.id}><div><strong>{user.login}{user.id===currentUser.id?' (you)':''}</strong>{user.name&&<span>{user.name}</span>}</div><span className="status-tag">{user.isAdmin?'Admin':'Learner'}</span>{user.id!==currentUser.id&&<button className="secondary" disabled={!!busy||loading} onClick={()=>void changeRole(user)}>{busy===user.id?'Saving…':user.isAdmin?'Remove admin':'Make admin'}</button>}</li>)}</ul>
  </section></main>
}
