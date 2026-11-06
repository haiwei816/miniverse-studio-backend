import{api}from'./client.mjs';export async function gm(name='admin'){return(await api('/gm/login','POST',{username:name,password:'sandbox-'+name})).access_token;}
