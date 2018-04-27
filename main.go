/*
--------------------------------------------------------------------------------
Company: -
Engineer: -

Create Date: 9.03.2018
Design Name:
Module Name:
Project Name: Elevator Project, TTK4145 (Real-time Programming)
Target Devices: Lift on Real-time lab or simulator v2
Tool versions: go1.9.2 windows/amd64
Description: Software for controlling n elevators working in parallel across m floors.

Dependencies: elevio, newtowrk (bcast, conn, localip, peers)

Revision: 1.0
Revision 0.01 - File Created
Aditional Coments:
--------------------------------------------------------------------------------
*/

package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"./elevio"
	"./network/bcast"
	"./network/localip"
	"./network/peers"
)

/*Constants Declarations*/
const (
	numFloors      = 4
	numButtons     = 3
	numHallButtons = 2
)

type ElevatorStatus int

const (
	ES_Idle     ElevatorStatus = 0
	ES_DoorOpen                = 1
	ES_Moving                  = 2
	ES_Error                   = 3
)

/*Custom Data type Declarations*/
type Elevator struct {
	Id        string
	Status    ElevatorStatus
	Floor     int
	Direction elevio.MotorDirection
	Request   []Request
}

type Order struct {
	Assigned string
	Request  Request
}

type Request struct {
	Floor  int
	Button elevio.ButtonType
}

/*Global Variable Declarations*/
var elevator Elevator
var elevators map[string]Elevator

/*
--------------------------------------------------------------------------------
Function: Main (fsm)
Description: Initializes hardware. Creates channels and starts go routines.
             Works as fsm running different functions in response to new data
             on the different channels.
Parameters:
Global: Altered: elevator Elevator - Elevator information.
        Altered: elevatos map[string]Elevator - Map of online lifts with their information.
Returns: Infinite loop.
See Also:
--------------------------------------------------------------------------------
*/
func main() {

	var id string

	flag.StringVar(&id, "id", "", "id of this peer")
	flag.Parse()

	if id == "" {
		localIP, err := localip.LocalIP()
		if err != nil {
			fmt.Println(err)
			localIP = "DISCONNECTED"
		}
		id = fmt.Sprintf("peer-%s-%d", localIP, os.Getpid())
	}

	elevio.Init("localhost:15657", numFloors)

	drv_buttons := make(chan elevio.ButtonEvent)
	drv_floors := make(chan int)
	drv_obstr := make(chan bool)
	drv_stop := make(chan bool)

	go elevio.PollButtons(drv_buttons)
	go elevio.PollFloorSensor(drv_floors)
	go elevio.PollObstructionSwitch(drv_obstr)
	go elevio.PollStopButton(drv_stop)

	elevators = make(map[string]Elevator)

	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)

	go peers.Transmitter(15647, id, peerTxEnable)
	go peers.Receiver(15647, peerUpdateCh)

	orderTx := make(chan Order)
	orderRx := make(chan Order)

	go bcast.Transmitter(16569, orderTx)
	go bcast.Receiver(16569, orderRx)

	elevatorTx := make(chan Elevator)
	elevatorRx := make(chan Elevator)

	go bcast.Transmitter(16569, elevatorTx)
	go bcast.Receiver(16569, elevatorRx)

	fsm_onInitBetweenFloors(numFloors, drv_floors)

	go elev_bcastElevatorInfo(elevatorTx)
	go fsm_setHallLights()
	go fsm_setCabLights()

	requestCh := make(chan Request, 10)

	doorTimeoutCh := make(chan bool)
	doorTimerResetCh := make(chan bool)

	go fsm_doorTimer(doorTimeoutCh, doorTimerResetCh)

	motorTimeoutCh := make(chan bool)
	motorTimerResetCh := make(chan bool)
	motorTimerStopCh := make(chan bool)

	go fsm_motorTimer(motorTimeoutCh, motorTimerResetCh, motorTimerStopCh)

	elevator.Id = id
	elevator.Status = ES_Idle

	fmt.Printf("\n\nElevator service started...\n\n")

	for {

		select {

		case reqBtn := <-drv_buttons:
			fmt.Printf("%+v\n", reqBtn)
			fsm_onRequestButtonPress(reqBtn, requestCh, orderTx)

		case floor := <-drv_floors:
			fmt.Printf("%+v\n", floor)
			fsm_onFloorArival(floor, doorTimerResetCh, motorTimerStopCh, motorTimerResetCh)

		case obstrBtn := <-drv_obstr:
			fmt.Printf("Obsruction! %+v\n", obstrBtn)

			if obstrBtn {
				elevio.SetMotorDirection(elevio.MD_Stop)
			} else {
				elevio.SetMotorDirection(elevator.Direction)
			}

		case stopBtn := <-drv_stop:
			fmt.Printf("%+v\n", stopBtn)

		case conn := <-peerUpdateCh:
			fmt.Printf("Peer update:\n")
			fmt.Printf("  Peers:    %q\n", conn.Peers)
			fmt.Printf("  New:      %q\n", conn.New)
			fmt.Printf("  Lost:     %q\n", conn.Lost)

			if conn.New != "" {
				elevators[conn.New] = Elevator{}

			} else if conn.Lost != nil {

				for _, i := range conn.Lost {
					// Inheritance hall requests from disconnected elevator
					for _, r := range elevators[i].Request {
						if r.Button == elevio.BT_HallUp || r.Button == elevio.BT_HallDown {
							requestCh <- r
						}
					}

					delete(elevators, i)
				}
			}

		case orderMsg := <-orderRx:
			fmt.Println("Recieved new request: ", orderMsg)
			if elevator.Id == orderMsg.Assigned {
				requestCh <- orderMsg.Request
			}

		case elevatorMsg := <-elevatorRx:
			fmt.Println("Recieved new elevator info:", elevatorMsg)

			// If the elevator is in the map
			_, isTrue := elevators[elevatorMsg.Id]
			if isTrue {
				// If the elevator has errors then heritage requests
				if elevatorMsg.Status == ES_Error && elevators[elevatorMsg.Id].Status != elevatorMsg.Status {
					for _, r := range elevatorMsg.Request {
						if r.Button == elevio.BT_HallUp || r.Button == elevio.BT_HallDown {
							requestCh <- r
						}
					}
				}

				elevators[elevatorMsg.Id] = elevatorMsg
			}

		case request := <-requestCh:
			fsm_onNewRequest(request, doorTimerResetCh, motorTimerResetCh)

		case <-doorTimeoutCh:
			fsm_onDoorTimeout(motorTimerResetCh)

		case <-motorTimeoutCh:
			fmt.Println("The elevator is stuck!")
			elevator.Status = ES_Error

		}
	}
}

/*
--------------------------------------------------------------------------------
Function: Assign Request to elevator
Description: If cab request, assign order to "local" elevator. If hall request
             assign "bestelevator" to order and transmitt.
Parameters: reqBtn elevio.ButtonEvent - Button Press (Floor, Button).
            requestCh chan<- Request - Assign order to local elevator.
            orderTx chan<- Order - Send request "best" elevator.
Global:
Returns:
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_onRequestButtonPress(reqBtn elevio.ButtonEvent, requestCh chan<- Request, orderTx chan<- Order) {
	r := Request{reqBtn.Floor, reqBtn.Button}

	switch reqBtn.Button {
	case elevio.BT_Cab:
		fmt.Println("Cab Button")
		requestCh <- r

	case elevio.BT_HallUp, elevio.BT_HallDown:
		fmt.Println("Hall Button")
		e := request_chooseBestElevator(reqBtn)
		orderMsg := Order{e, r}
		orderTx <- orderMsg

	}
}

/*
--------------------------------------------------------------------------------
Function: Accept request
Description: Add new order to the order list and start the elevator if idle.
Parameters: newRequest Request - New Request (Floor, Button).
            doorTimerResetCh (chan<-bool) - Reset door timer.
            motorTimerResetpCh (chan<-bool) - Reset motor timer.
Global: Altered: elevator Elevator - Elevator information.
Returns:
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_onNewRequest(newRequest Request, doorTimerResetCh chan<- bool, motorTimerResetCh chan<- bool) {
	fmt.Println("I was assigned a new request", newRequest)

	switch elevator.Status {
	case ES_Idle:
		if elevator.Floor == newRequest.Floor {
			elevio.SetDoorOpenLamp(true)
			doorTimerResetCh <- true
		} else {
			elevator.Request = append(elevator.Request, newRequest)
			d := request_chooseDirection()
			elevator.Direction = d
			elevio.SetMotorDirection(d)
			motorTimerResetCh <- true
			elevator.Status = ES_Moving
		}

	case ES_DoorOpen:
		if elevator.Floor == newRequest.Floor {
			doorTimerResetCh <- true
		} else {
			elevator.Request = append(elevator.Request, newRequest)
		}

	case ES_Moving, ES_Error:
		elevator.Request = append(elevator.Request, newRequest)

	}
}

/*
--------------------------------------------------------------------------------
Function: Choose Direction
Description: Select the direction of the elevator
Parameters:
Global: Altered: elevator Elevator - Elevator information.
Returns: elevio.MotorDirection
See Also: <fsm_onNewRequest>
--------------------------------------------------------------------------------
*/
func request_chooseDirection() elevio.MotorDirection {
	var d elevio.MotorDirection

	if len(elevator.Request) != 0 {
		if elevator.Request[0].Floor > elevator.Floor {
			d = elevio.MD_Up
		} else if elevator.Request[0].Floor < elevator.Floor {
			d = elevio.MD_Down
		} else {
			d = elevio.MD_Stop
		}
	}

	return d
}

/*
--------------------------------------------------------------------------------
Function: On Floor Arival
Description: Update current floor. If should stop then stop, clear requests
             and open door. If error then recover from error.
Parameters: newFloor (int) - The elevator's new floor
            doorTimerResetCh (chan<-bool) - Reset door timer.
            motorTimerStopCh (chan<-bool) - Stop motor timer.
            motorTimerResetpCh (chan<-bool) - Reset motor timer.
Global: Altered: elevator Elevator - Elevator information.
Returns:
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_onFloorArival(newFloor int, doorTimerResetCh chan<- bool, motorTimerStopCh chan<- bool, motorTimerResetCh chan<- bool) {

	elevator.Floor = newFloor
	elevio.SetFloorIndicator(newFloor)

	switch elevator.Status {
	case ES_Moving:
		if request_shouldStop(newFloor) {
			fsm_stopElevatorClearRequestsOpenDoor(newFloor, doorTimerResetCh, motorTimerStopCh)

		} else if elevator.Status != ES_Idle {
			motorTimerResetCh <- true
		}

	case ES_Error:
		//Remove failed Hall requests
		for i := 0; i < len(elevator.Request); i++ {
			if elevator.Request[i].Button == elevio.BT_HallUp || elevator.Request[i].Button == elevio.BT_HallDown {
				elevator.Request = append(elevator.Request[:i], elevator.Request[i+1:]...)
				i--
			}
		}
		//If cab order then execute else stop
		if len(elevator.Request) > 0 {
			if request_shouldStop(newFloor) {
				fsm_stopElevatorClearRequestsOpenDoor(newFloor, doorTimerResetCh, motorTimerStopCh)
			} else {
				motorTimerResetCh <- true
			}

		} else {
			elevio.SetMotorDirection(elevio.MD_Stop)
			elevator.Direction = elevio.MD_Stop
			elevator.Status = ES_Idle
		}
	}
}

/*
--------------------------------------------------------------------------------
Function: Stop Elevator, clear request and Open Door
Description: Stop Elevator, clear request and Open Door
Parameters: newFloor (int) - The elevator's new floor
            doorTimerResetCh (chan<-bool) - Reset door timer.
            motorTimerStopCh (chan<-bool) - Stop motor timer.
Global: Altered: elevator Elevator - Elevator information.
Returns:
See Also: <fsm_onFloorArival>
--------------------------------------------------------------------------------
*/
func fsm_stopElevatorClearRequestsOpenDoor(newFloor int, doorTimerResetCh chan<- bool, motorTimerStopCh chan<- bool) {
	elevio.SetMotorDirection(elevio.MD_Stop)
	request_clearAtCurrenFloor(newFloor)
	elevio.SetDoorOpenLamp(true)
	doorTimerResetCh <- true
	motorTimerStopCh <- true
	elevator.Status = ES_DoorOpen
}

/*
--------------------------------------------------------------------------------
Function: Decide whether the lift should stop.
Description: Stop elevator if request at CurrenFloor.
Parameters: newFloor int - The elevator's new floor
Global:
Returns:
See Also: <fsm_onFloorArival>
--------------------------------------------------------------------------------
*/
func request_shouldStop(newFloor int) bool {
	for _, r := range elevator.Request {
		if newFloor == r.Floor {
			return true
		}
	}
	return false
}

/*
--------------------------------------------------------------------------------
Function: Clear Request At CurrenFloor.
Description: Clear Request At CurrenFloor.
Parameters: newFloor int - The elevator's new floor
Global: Altered: elevator Elevator - Elevator information.
Returns:
See Also: <fsm_stopElevatorClearRequestOpenDoor<
--------------------------------------------------------------------------------
*/
func request_clearAtCurrenFloor(newFloor int) {
	for i := 0; i < len(elevator.Request); i++ {
		if newFloor == elevator.Request[i].Floor {
			elevator.Request = append(elevator.Request[:i], elevator.Request[i+1:]...)
			i--
		}
	}
}

/*
--------------------------------------------------------------------------------
Function: Door Timer
Description: Door timeout if timer expires
Parameters: timeout (chan bool) - Door timer timed out.
            reset (chan bool) - Reset doortimer.
Global:
Returns:
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_doorTimer(timeout chan<- bool, reset <-chan bool) {
	timer := time.NewTimer(0)
	timer.Stop()

	for {
		select {
		case <-reset:
			timer.Reset(3 * time.Second)
		case <-timer.C:
			timer.Stop()
			timeout <- true
		}
	}
}

/*
--------------------------------------------------------------------------------
Function: Motor Timer
Description: Motor timeout if timer expires
Parameters: timeout (chan bool) - Motor timer timed out.
            start (chan bool) - Start motor timer.
            stop (chan bool) - Stop motor timer.
Global:
Returns: Infinite loop.
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_motorTimer(timeout chan<- bool, start <-chan bool, stop <-chan bool) {
	timer := time.NewTimer(0)
	timer.Stop()

	for {
		select {
		case <-start:
			timer.Reset(3 * time.Second)

		case <-stop:
			timer.Stop()

		case <-timer.C:
			timer.Stop()
			timeout <- true
		}
	}
}

/*
--------------------------------------------------------------------------------
Function: Close the door and continue
Description: Close the door and if there are more orders continue elese idle.
Parameters: motorTimerResetCh (chan bool) - motor Timer Reset Channel.
Global: Altered: elevator Elevator - Elevator information.
Returns:
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_onDoorTimeout(motorTimerResetCh chan<- bool) {
	elevio.SetDoorOpenLamp(false)

	d := request_chooseDirection()
	elevator.Direction = d
	elevio.SetMotorDirection(d)

	if d == elevio.MD_Stop {
		elevator.Status = ES_Idle
	} else {
		elevator.Status = ES_Moving
		motorTimerResetCh <- true
	}
}

/*
--------------------------------------------------------------------------------
Function: Place the elevator in a known state.
Description: Reset all lights and drive the elevator to the floor below.
Parameters: numFloors int - Number of floors
            floor <-chan int - Floor indicator
Global: Altered: elevator Elevator - Elevator information.n.
Returns:
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_onInitBetweenFloors(numFloors int, floor <-chan int) {
	for f := 0; f < numFloors; f++ {
		for b := elevio.ButtonType(0); b < 3; b++ {
			elevio.SetButtonLamp(b, f, false)
		}
	}

	elevio.SetDoorOpenLamp(false)
	elevio.SetStopLamp(false)

	elevio.SetMotorDirection(elevio.MD_Down)

	f := <-floor

	elevator.Floor = f
	elevio.SetFloorIndicator(f)
	elevio.SetMotorDirection(elevio.MD_Stop)

}

/*
--------------------------------------------------------------------------------
Function: Choose Best Elevator
Description: Return the id to the lift that is best suited to complete this request.
Parameters: reqBtn elevio.ButtonEvent - Floor and Button
Global: elevatos map[string]Elevator - Map of online lifts with their information.
Returns: string
See Also: <onRequestButtonPress>
--------------------------------------------------------------------------------
*/
func request_chooseBestElevator(reqBtn elevio.ButtonEvent) string {
	var bestCost, cost float64 = -999, 0
	var id string

	for _, e := range elevators {

		cost = 3 - math.Abs(float64(reqBtn.Floor-e.Floor))

		for _, r := range e.Request {
			// If request in the opposite direction on the same floor
			if r.Floor == reqBtn.Floor && r.Button != reqBtn.Button && r.Button != elevio.BT_Cab {
				cost -= 10
			} else if r.Button == elevio.BT_Cab {
				// If the request is in the opposite direction of the direction the elevator is running
				if reqBtn.Button == elevio.BT_HallUp && e.Floor >= reqBtn.Floor && reqBtn.Floor > r.Floor || reqBtn.Button == elevio.BT_HallDown && e.Floor <= reqBtn.Floor && reqBtn.Floor < r.Floor {
					cost -= 10
				}
			}
		}

		//if the elevator is driving away from the request
		if e.Direction == elevio.MD_Up && e.Floor >= reqBtn.Floor || e.Direction == elevio.MD_Down && e.Floor <= reqBtn.Floor {
			cost -= 10
		}

		if e.Status == ES_Error {
			cost -= 100
		}

		fmt.Println(cost, e.Id)

		if cost >= bestCost {
			bestCost = cost
			id = e.Id

		}
	}

	return id
}

/*
--------------------------------------------------------------------------------
Function: Brodcast Elevator Information
Description: Every 100 millisecond brodcast Elevator Information.
Parameters: elevatorTx (chan Elevator) - Elevator channel
Global: elevator Elevator - Elevator information.
Returns: Infinite loop.
See Also: <main>
--------------------------------------------------------------------------------
*/
func elev_bcastElevatorInfo(elevatorTx chan<- Elevator) {
	for {
		elevatorMsg := Elevator{elevator.Id, elevator.Status, elevator.Floor, elevator.Direction, elevator.Request}
		elevatorTx <- elevatorMsg
		time.Sleep(100 * time.Millisecond)
	}
}

/*
--------------------------------------------------------------------------------
Function: Set Cab Button Lamp.
Description: Set Cab Button Lamp if the elevator has an order.
Parameters:
Global: elevator (Elevator) - Elevator information.
Returns: Infinite loop.
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_setCabLights() {
	var requests [numFloors][numButtons]int

	for {

		for _, r := range elevator.Request {
			requests[r.Floor][r.Button] = 1
		}

		for f := 0; f < numFloors; f++ {
			if requests[f][elevio.BT_Cab] == 1 {
				elevio.SetButtonLamp(elevio.BT_Cab, f, true)
			} else {
				elevio.SetButtonLamp(elevio.BT_Cab, f, false)
			}
			requests[f][elevio.BT_Cab] = 0
		}

		time.Sleep(500 * time.Millisecond)
	}
}

/*
--------------------------------------------------------------------------------
Function: Set Hall Button Lamp.
Description: Set Hall Button Lamp if one or more elevators have an avtive order
             and there are at least two lifts online.
Parameters:
Global: elevatos map[string]Elevator - Map of online lifts with their information.
Returns: Infinite loop.
See Also: <main>
--------------------------------------------------------------------------------
*/
func fsm_setHallLights() {
	var requests [numFloors][numButtons]int

	for {

		for _, e := range elevators {
			if e.Status != ES_Error {
				for _, r := range e.Request {
					requests[r.Floor][r.Button] = 1
				}
			}
		}

		for f := 0; f < numFloors; f++ {
			for b := elevio.ButtonType(0); b < numHallButtons; b++ {
				if requests[f][b] == 1 {
					if len(elevators) >= 2 {
						elevio.SetButtonLamp(b, f, true)
					}
				} else {
					elevio.SetButtonLamp(b, f, false)
				}
				requests[f][b] = 0
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}
