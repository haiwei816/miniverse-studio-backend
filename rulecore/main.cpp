#include "rules.hpp"
#include "rule.grpc.pb.h"
#include <grpcpp/grpcpp.h>
#include <iostream>
#include <string>
#include <vector>
class Server final:public miniverse::rule::v1::RuleCore::Service{
 grpc::Status Replay(grpc::ServerContext* ctx,const miniverse::rule::v1::ReplayRequest* request,miniverse::rule::v1::ReplayResult* reply)override{
  std::string req_id;
  std::string trace_tp;
  if(ctx!=nullptr){
   const auto& md=ctx->client_metadata();
   if(auto it=md.find("x-request-id");it!=md.end()){req_id.assign(it->second.data(),it->second.size());}
   if(auto it=md.find("traceparent");it!=md.end()){trace_tp.assign(it->second.data(),it->second.size());}
  }
  std::vector<std::uint32_t> actions(request->actions().begin(),request->actions().end());
  const auto r=miniverse::replay(request->version(),request->seed(),actions);
  if(!req_id.empty()||!trace_tp.empty()){
   std::cout<<"rulecore_replay request_id="<<req_id<<" traceparent="<<trace_tp<<" version="<<request->version()<<" valid="<<r.valid<<" score="<<r.score<<"\n";
  }
  reply->set_valid(r.valid);reply->set_score(r.score);reply->set_state_hash(r.hash);reply->set_error(r.error);reply->set_finished(r.finished);reply->set_victory(r.victory);return grpc::Status::OK;
 }
};
int main(){Server service;grpc::ServerBuilder builder;builder.SetMaxReceiveMessageSize(65536);builder.AddListeningPort("0.0.0.0:50051",grpc::InsecureServerCredentials());builder.RegisterService(&service);auto server=builder.BuildAndStart();if(!server)return 1;std::cout<<"RuleCore card-v1 listening internally on 50051"<<std::endl;server->Wait();}
