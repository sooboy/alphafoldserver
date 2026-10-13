(async()=>{
  // Derived from scripts/page-job-status.js. Tokens stay in the AlphaFold page.
  // __AF_POOL_REQUEST__ is a lexical argument supplied by the local worker.
  const command=typeof __AF_POOL_REQUEST__==='undefined'?null:__AF_POOL_REQUEST__;
  const allowed=['EU26qc','tjI49c','IbBsAc','Gbje0','kMnDgb','bteK7','EXvXWc'];
  const rpc=command?.rpc??'IbBsAc';
  if(!allowed.includes(rpc))return {ok:false,error:'UNSUPPORTED_RPC'};
  const b=window.WIZ_global_data;
  if(!b?.SNlM0e)return {ok:false,error:'NEEDS_LOGIN'};
  const query=new URLSearchParams({rpcids:rpc,'source-path':location.pathname,
    bl:b.cfb2h,'f.sid':b.FdrFJe,hl:'en',_reqid:String(Date.now()%10000000),rt:'c'});
  const form=new URLSearchParams({'f.req':JSON.stringify([[[rpc,JSON.stringify(command?.payload??[]),null,'1']]]),at:b.SNlM0e});
  let response,text;
  try{
    response=await fetch('/_/AlphaFoldUi/data/batchexecute?'+query,{method:'POST',
      credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded;charset=UTF-8','X-Same-Domain':'1'},
      body:form,signal:AbortSignal.timeout(25000)});
    if(!response.ok)return {ok:false,error:response.status===429?'RATE_LIMITED':response.status===401?'NEEDS_LOGIN':'HTTP_ERROR',http_status:response.status};
    text=await response.text();
  }catch{return {ok:false,error:'TRANSPORT_UNKNOWN'};}
  // JSON-aware complete-frame parser: ignore numeric framing lines, never eval.
  let p=text.startsWith(")]}'")?4:0;const rows=[];
  try{
    while(p<text.length){
      while(p<text.length&&/\s/.test(text[p]))p++;
      if(p===text.length)break;
      const length=/^\d+\r?\n/.exec(text.slice(p));if(length){p+=length[0].length;continue;}
      if(text[p]!=='[')throw Error();
      const start=p;let depth=0,quoted=false,escape=false,closed=false;
      for(;p<text.length;p++){
        const c=text[p];
        if(quoted){if(escape)escape=false;else if(c==='\\')escape=true;else if(c==='"')quoted=false;}
        else if(c==='"')quoted=true;
        else if(c==='['||c==='{')depth++;
        else if(c===']'||c==='}')if(--depth===0){rows.push(...JSON.parse(text.slice(start,++p)));closed=true;break;}
      }
      if(!closed)throw Error();
    }
    const record=rows.find(r=>Array.isArray(r)&&r[0]==='wrb.fr'&&r[1]===rpc);
    if(!record)throw Error();
    const code=record[5]?.[0];
    if(typeof code==='number'&&code!==0)return {ok:false,error:code===16?'NEEDS_LOGIN':code===8?'RATE_LIMITED':'RPC_ERROR',rpc_error:code};
    if(typeof record[2]!=='string')throw Error();
    const payload=JSON.parse(record[2]);
    if(command?.kind==='identity'){
      if(rpc!=='EU26qc'||typeof payload[0]!=='string'||!payload[0])throw Error();
      const bytes=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(payload[0]));
      return {ok:true,identity_hash:Array.from(new Uint8Array(bytes),x=>x.toString(16).padStart(2,'0')).join('')};
    }
    if(command)return {ok:true,payload,http_status:response.status};
    const matches=(payload[0]??[]).filter(j=>j[6]?.[0]==='af-protocol-draft-001');
    if(matches.length!==1)throw Error();const j=matches[0];
    return {job_name:'af-protocol-draft-001',job_id:j[0][0],status:j[1],model_count:j[7]?.length??0,
      has_error:Boolean(j[4]),error_code:j[4]?.[0]??null,http_status:response.status,checked_at:new Date().toISOString()};
  }catch{return {ok:false,error:'PROTOCOL_CHANGED'};}
})()
