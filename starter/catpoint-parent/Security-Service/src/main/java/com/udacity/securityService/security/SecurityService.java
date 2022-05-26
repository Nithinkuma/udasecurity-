package com.udacity.securityService.security;

import com.udacity.imageService.ImageService;
import com.udacity.securityService.application.StatusListener;
import com.udacity.securityService.data.AlarmStatus;
import com.udacity.securityService.data.ArmingStatus;
import com.udacity.securityService.data.SecurityRepository;
import com.udacity.securityService.data.Sensor;


import java.awt.image.BufferedImage;
import java.util.HashSet;
import java.util.Set;


/**
 * Service that receives information about changes to the security system. Responsible for
 * forwarding updates to the repository and making any decisions about changing the system state.
 *
 * This is the class that should contain most of the business logic for our system, and it is the
 * class you will be writing unit tests for.
 */
public class SecurityService {

    private ImageService imageService;
    private SecurityRepository securityRepository;
    private Set<StatusListener> statusListeners = new HashSet<>();

    boolean detectedCat = false;

    public SecurityService(SecurityRepository securityRepository, ImageService imageService) {
        this.securityRepository = securityRepository;
        this.imageService = imageService;
    }

    /**
     * Sets the current arming status for the system. Changing the arming status
     * may update both the alarm status.
     * @param armingStatus
     */
    public void setArmingStatus(ArmingStatus armingStatus) {

        if (isCatDetect() && armingStatus==ArmingStatus.ARMED_HOME){
            setAlarmStatus(AlarmStatus.ALARM);
        }
        else if(armingStatus == ArmingStatus.DISARMED) {
            setAlarmStatus(AlarmStatus.NO_ALARM);
        }
        else{
            Set<Sensor> sensors = new HashSet<>(getSensors());
            sensors.forEach(sensor -> changeSensorActivationStatus(sensor,false));
        }
        securityRepository.setArmingStatus(armingStatus);
        statusListeners.forEach(StatusListener :: sensorStatusChanged);
    }

    boolean isCatDetect() {
        return detectedCat;
    }
    public void setCatDetect(boolean detectedCat) {
        this.detectedCat = detectedCat;
    }

    /**
     * Internal method that handles alarm status changes based on whether
     * the camera currently shows a cat.
     * @param cat True if a cat is detected, otherwise false.
     */
    private void catDetected(Boolean cat) {
        setCatDetect(cat);
        if( isCatDetect() && getArmingStatus() == ArmingStatus.ARMED_HOME) {
            setAlarmStatus(AlarmStatus.ALARM);
        } else if(!cat && getSensorActivity()) {
            setAlarmStatus(AlarmStatus.NO_ALARM);
        }
        statusListeners.forEach(sl -> sl.catDetected(cat));
    }

    private boolean getSensorActivity(){
        return getSensors().stream().noneMatch(Sensor::getActive);
    }

    /**
     * Register the StatusListener for alarm system updates from within the SecurityService.
     * @param statusListener
     */
    public void addStatusListener(StatusListener statusListener) {
        statusListeners.add(statusListener);
    }

    public void removeStatusListener(StatusListener statusListener) {
        statusListeners.remove(statusListener);
    }

    /**
     * Change the alarm status of the system and notify all listeners.
     * @param status
     */
    public void setAlarmStatus(AlarmStatus status) {
        securityRepository.setAlarmStatus(status);
        statusListeners.forEach(sl -> sl.notify(status));
    }

    /**
     * Internal method for updating the alarm status when a sensor has been activated.
     */
    private void handleSensorActivated() {
        if(securityRepository.getArmingStatus() == ArmingStatus.DISARMED) {
            return; //no problem if the system is disarmed
        }
        switch(securityRepository.getAlarmStatus()) {
            case NO_ALARM->setAlarmStatus(AlarmStatus.PENDING_ALARM);
            case PENDING_ALARM,ALARM->setAlarmStatus(AlarmStatus.ALARM);
        }
    }

    /**
     * Internal method for updating the alarm status when a sensor has been deactivated
     */

    void handleSensorDeactivated() {
        switch(securityRepository.getAlarmStatus()) {
            case PENDING_ALARM -> setAlarmStatus(AlarmStatus.NO_ALARM);
            case ALARM -> setAlarmStatus(AlarmStatus.ALARM);
        }
    }

    /**
     * Change the activation status for the specified sensor and update alarm status if necessary.
     * @param sensor
     * @param active
     */
    public void changeSensorActivationStatus(Sensor sensor,boolean active) {
        AlarmStatus alarmStatus = securityRepository.getAlarmStatus();
        if(alarmStatus!=AlarmStatus.ALARM) {
            if (!sensor.getActive() && active) {
                handleSensorActivated();
            } else if (sensor.getActive() && !active) {
                handleSensorDeactivated();
            }
        }
        if(alarmStatus==AlarmStatus.ALARM){
            if(sensor.getActive() && !active)
                handleSensorDeactivated();
        }
        sensor.setActive(active);
        securityRepository.updateSensor(sensor);
    }


    /**
     * Send an image to the SecurityService for processing. The securityService will use its provided
     * ImageService to analyze the image for cats and update the alarm status accordingly.
     * @param currentCameraImage
     */
    public void processImage(BufferedImage currentCameraImage) {
        catDetected(imageService.imageContainsCat(currentCameraImage, 50.0f));
    }

    public AlarmStatus getAlarmStatus() {
        return securityRepository.getAlarmStatus();
    }

    public Set<Sensor> getSensors() {
        return securityRepository.getSensors();
    }

    public void addSensor(Sensor sensor) {
        securityRepository.addSensor(sensor);
    }

    public void removeSensor(Sensor sensor) {
        securityRepository.removeSensor(sensor);
    }

    public ArmingStatus getArmingStatus() {
        return securityRepository.getArmingStatus();
    }
}
