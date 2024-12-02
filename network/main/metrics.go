package main

// collects metrics
type metrics struct { 

}

// defaultpongtimeout 
// if lastSent > lastRecieved + constants.DefaultPongTimeout
// 		push(time, offline)
// else if (offline) {
// 		push(time, online)
// }