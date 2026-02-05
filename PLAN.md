Our api is entirely .proto files. found here: proto/discopanel/v1/*.proto                                                                                                                                                                                             
These protos are compiled into protobuf generated Go (for backend) and Typescript (for frontend). This retains a single ultimate source of truth, the protos.                                                                                                         
                                                                                                                                                                                                                                                                
Now, I would like to continue in that spirit with the following: Since clients can only make requests, our UI relies entirely on polling our connectrpc http endpoints (using protobuf + connectrpc), but it adds enormous overhead to our front end and is very      
difficult to keep things smooth and reactive. The solution, we completely eliminate polling. Instead, we do the following:                                                                                                                                            
                                                                                                                                                                                                                                                                
For all of our RPC's, we previously would make a request like "GetServer" -> wait ~10 seconds -> GetServer -> .....                                                                                                                                   
Each 10 seconds we'd update the server object state rendered on what was a Server[id] page we were looking at (or a user was).                                                                                                                                        
                                                                                                                                                                                                                                                                    
The solution for this: Client makes the same initial http/connectrpc request to GetServer (or any of the other RPC service endpoint) with the header X-KEEP-ALIVE (or something better idk), but instead of         
polling the server, the server sends back the expected data but in it's header in response, it has X-ALIVE - containing a topic to subscribe to for updates. Client sees this and subscribes via it's websocket, the message from the server uses the same exact connectrpc proto struct that the initial http response used. It is seamless.

The topic name doesnt even matter, it could be a uuid, it has no relevance. We initiated the subscription during the http request, THAT IS HOW WE KNOW WHAT WE WILL GET BACK AND WHERE. 

FOR EXAMPLE. The first request when we load the page, calls the below rpc. 

rpc GetServer(GetServerRequest) returns (GetServerResponse);

Client - GetServerRequest -> Server - GetServerResponse -> Client (same client)

If the client sent X-KEEP-ALIVE in the header with its request, the server is sending back some subscription topic in its header in the response. 

NOW THE MOST IMPORTANT PART AND THE ONLY WAY THIS WORKS: 

Client - GetServerRequest (connectrpc/http) + X-KEEP-ALIVE: true -> Server - GetServerResponse (connectrpc/http) X-ALIVE: 123456 -> Client (same client)
Client's Websocket - Subscribe to 12345 -> Server
Server's Websocket - Ack 12345 -> Client
....
.... A server update happens, or player joins, or something - an emitter is triggered ....
Server's Websocket - Update 12345 (GetServerResponse) -> Client acts the same exact way it did when the server sent its initial GetServerResponse to the connectrpc/http GetServerRequest. Client updates it's Server object using the same exact serialized struct.
                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               
The final part is "Emitters", the source of server sent websocket message updates to our client subscribers - ie: what is responding to clients. Also auth will be very easy if auth is enabled. ALL YOU NEED TO DO IS RE-USE THE SAME EXACT TOKEN THAT IS LITERALLY ALREADY STORED FROM THE INITIAL HTTP/CONNECTRPC REQUEST. I REPEAT NO AUTH IS EVER INITIATED OVER WEBSOCKET. NO ADDITIONAL AUTH CODE IS NEEDED. YOU PATCH IN THE SAME EXACT TOKEN DURING SUBSCRIBE AND IT GETS VALIDATED THE SAME WAY ANY OTHER REQUEST ALREADY IS. 
                                                                                                                                                                                                                                                                    
Again just to reiterate, THE ONLY THING CHANGING BETWEEN THE INITIAL HTTP REQUEST/RESPONSE AND THE WEBSOCKET SUB/PUB IS THE TRANSPORT. THE EXISTING PROTOBUFS ARE STILL BEING SENT OVER THE WIRE. EACH WEBSOCKET MESSAGE's CONTENT TO A CLIENT WILL LOOK EXACTLY      
AS IF THE SERVER JUST SENT THE CLIENT AN HTTP RESPONSE. It's connect rpc but over websocket instead of grpc/http.