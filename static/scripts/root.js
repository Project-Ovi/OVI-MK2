const webcam = document.getElementById("webcam");
const cameras_container = document.getElementById("cameras");
const used_camera = document.getElementById("camera-selector");
const drop_down_button = document.getElementById("used-camera");
const drop_down = document.getElementById("cameras");
const drop_icon = document.getElementById("drop-button");
const CxDisp = document.getElementById("Cx");
const CyDisp = document.getElementById("Cy");
const toggleButton = document.getElementById("button");
const coorddisp = document.getElementById("coorddisp");
const ctrlPanel = document.getElementById("controlPanel");
const warnPanel = document.getElementById("warning");
const warnTitle = warnPanel.getElementsByClassName("title")[0];
const warnDescription = warnPanel.getElementsByClassName("description")[0];

let websocket = new WebSocket("/ws");;
let cameras, Cx, Cy, manual=0, camera_index, homing=0;

var dropped_down = true;
let drop_HTML;

function connect() {
    websocket = new WebSocket("/ws");
}

websocket.onmessage = function(event) {

    switch(event.data.substring(0, 3)) {
        case "CAM":
            webcam.src = "data:image/png;base64,"+event.data.substring(3);
            break;
        case "CAS":
            if (dropped_down) {
                break;
            }
            cameras = event.data.substring(3).split("|");

            cameras_container.innerHTML = "";
            for (let i=0; i < cameras.length; i++) {
                let child = document.createElement('div');
                child.onclick = function() {
                    toggle_drop_down();
                    websocket.send("CAM" + i);
                }
                child.textContent = cameras[i];
                child.classList.add("camera-entry")
                child.style.display = "none";

                cameras_container.appendChild(child);
            }
            drop_HTML = cameras_container.innerHTML;
            
            break;
        case "CXD":
            Cx = event.data.substring(3);
            CxDisp.textContent = Cx;
            break;
        case "CYD":
            Cy = event.data.substring(3);
            CyDisp.textContent = Cy;
            break;
        case "MAN":
            manual = event.data.substring(3);
            if (manual == "1") {
                coorddisp.style.display = "none";
                ctrlPanel.style.display = "flex";
            } else if (manual == 0) {
                coorddisp.style.display = "flex";
                ctrlPanel.style.display = "none";
            }
            break;
        case "CON":
            camera_index = event.data.substring(3);
            used_camera.textContent = cameras[camera_index];
            break;
        case "HOM":
            homing = event.data.substring(3);
            if (homing != 0) {
                warnPanel.style.display = "flex";
                warnDescription.textContent = homing
            } else {
                warnPanel.style.display = "none";
            }
    }
}

websocket.onclose = function() {
    console.log("Connection dropped! Attempting to reconnect...");
    setTimeout(connect, 100);
}

websocket.onerror = websocket.onclose

function toggle_drop_down () {
    dropped_down = !dropped_down;

    if (dropped_down) {
        drop_down.style.height = (cameras.length * (35 + 2 * 10)) + 2 * 10 + "px";
        drop_down.style.padding = "10px";
        setTimeout(flexChildren, 500, drop_down);
        drop_icon.style.transform = "rotate(180deg)";
    } else {
        drop_down.style.height = "0";
        stopChildren(drop_down);
        drop_down.style.padding = "0";
        drop_icon.style.transform = "rotate(0deg)";
    }
}

function stopChildren(parent) {
    for (let i=0; i < parent.children.length; i++) {
        parent.children[i].style.display = "none"
    }
}
function flexChildren(parent) {
    for (let i=0; i < parent.children.length; i++) {
        parent.children[i].style.display = "flex"
    }
}

toggle_drop_down();

drop_down_button.onclick = toggle_drop_down;

toggleButton.onclick = function() {
    websocket.send("MAN" + Math.abs(manual-1));
}

document.getElementById("forward-ctrl").onclick = function () {
    websocket.send("CTR"+"F")
}

document.getElementById("backward-ctrl").onclick = function () {
    websocket.send("CTR"+"B")
}

document.getElementById("left-ctrl").onclick = function () {
    websocket.send("CTR"+"L")
}

document.getElementById("right-ctrl").onclick = function () {
    websocket.send("CTR"+"R")
}

document.getElementById("up-ctrl").onclick = function () {
    websocket.send("CTR"+"U")
}

document.getElementById("down-ctrl").onclick = function () {
    websocket.send("CTR"+"D")
}