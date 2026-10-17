(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const params = new URLSearchParams(location.search);
  const code = params.get('code');
  const isPair = location.pathname === '/pair';
  history.replaceState(null, '', location.pathname); // remove sensitive invitation from URL after reading
  $('pair-view').hidden = !isPair;
  $('app-view').hidden = isPair;
  const msg = (text) => { $('msg').textContent = text; };
  const fmt = (n) => n < 1024 ? `${n} B` : n < 1048576 ? `${(n/1024).toFixed(1)} KiB` : `${(n/1048576).toFixed(1)} MiB`;
  async function request(url, opts = {}) {
    const resp = await fetch(url, {...opts, cache:'no-store', credentials:'same-origin'});
    if (!resp.ok) {
      const body = await resp.json().catch(() => ({}));
      throw new Error(body.error || `HTTP ${resp.status}`);
    }
    return resp;
  }
  if (isPair) {
    $('pair-btn').addEventListener('click', async () => {
      try {
        if (!code) throw new Error('Missing invitation. Generate a fresh QR on your Arch computer.');
        await request('/api/pair/redeem', { method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({code}) });
        location.replace('/');
      } catch (e) { $('pair-msg').textContent = e.message; }
    });
    return;
  }
  async function load() {
    const parent = $('file-items'); parent.replaceChildren();
    try {
      const data = await (await request('/api/files')).json();
      if (!data.length) { const empty=document.createElement('p');empty.className='empty';empty.textContent='No files yet. Send your first file above.';parent.append(empty); }
      for (const item of data) {
        const row = document.createElement('div');row.className='file';
        const detail=document.createElement('div');detail.className='details';
        const name=document.createElement('strong');name.textContent=item.name;
        const sub=document.createElement('small');sub.textContent=fmt(item.size);
        detail.append(name,sub);
        const actions=document.createElement('div');actions.className='actions';
        const download=document.createElement('button');download.textContent='Download';
        download.addEventListener('click', () => { location.href=`/api/files/${encodeURIComponent(item.id)}/download`; });
        const remove=document.createElement('button');remove.className='danger';remove.textContent='Delete';
        remove.addEventListener('click',async()=>{
          if (!confirm(`Delete ${item.name}?`)) return;
          try {await request(`/api/files/${encodeURIComponent(item.id)}`,{method:'DELETE'});await load();}
          catch(e){msg(e.message)}
        });
        actions.append(download,remove);row.append(detail,actions);parent.append(row);
      }
      msg('');
    } catch(e){msg(e.message === 'pair with BIfrost first' ? 'This device is not paired. Scan a new QR from the Arch computer.' : e.message);}
  }
  $('refresh-btn').addEventListener('click',load);
  $('file-picker').addEventListener('change',async(event)=>{
    const selected=Array.from(event.target.files || []);
    for (const file of selected) {
      const row=document.createElement('div');row.className='file';
      const details=document.createElement('div');details.className='details';
      const name=document.createElement('strong');name.textContent=file.name;
      const bar=document.createElement('div');bar.className='bar';const fill=document.createElement('div');fill.className='fill';bar.append(fill);
      const state=document.createElement('small');state.textContent='Waiting…';details.append(name,bar,state);row.append(details);$('upload-items').append(row);
      if(file.size > 512*1024*1024){state.textContent='File larger than 512 MiB';continue;}
      try {
        await new Promise((resolve,reject)=>{
          const xhr=new XMLHttpRequest();
          xhr.open('POST',`/api/files?name=${encodeURIComponent(file.name)}`);
          xhr.upload.onprogress=e=>{if(e.lengthComputable)fill.style.width=`${Math.round(e.loaded/e.total*100)}%`};
          xhr.onload=()=>xhr.status===201?resolve():reject(new Error(`Upload failed: HTTP ${xhr.status}`));
          xhr.onerror=()=>reject(new Error('Connection lost'));
          xhr.send(file);
        });
        state.textContent='Uploaded';fill.style.width='100%';
      } catch(e){state.textContent=e.message}
    }
    event.target.value='';await load();
  });
  load();
})();
