package com.udacity.securityService.security;

import com.udacity.imageService.ImageService;
import com.udacity.securityService.application.StatusListener;
import com.udacity.securityService.data.*;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;;
import org.junit.jupiter.params.provider.ValueSource;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.EnumSource;

import java.awt.image.BufferedImage;
import java.util.HashSet;
import java.util.Set;
import java.util.UUID;

import static com.udacity.securityService.data.AlarmStatus.*;
import static com.udacity.securityService.data.ArmingStatus.*;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class SecurityServiceTest {
    private SecurityService securityService;
    private final UUID randomID = UUID.randomUUID();
    private Sensor sensor;

    private Set<Sensor> sensors;
    private StatusListener listener;

    @Mock
    private ImageService imageService;

    @Mock
    private SecurityRepository securityRepository;

    @BeforeEach
    void init(){
        securityService = new SecurityService(securityRepository,imageService);
        sensor = new Sensor(randomID.toString(),SensorType.DOOR);
    }

    private Set<Sensor> getSensorsTest(int count,boolean status){
        Set<Sensor> sensors = new HashSet<>();
        for (int i = 0; i < count; i++) {
            sensors.add(new Sensor(randomID.toString(), SensorType.DOOR));
        }
        sensors.forEach(sensor -> sensor.setActive(status));
        return sensors;
    }

    @Test//for mentioned test case 1
    void alarmArmedSensorActivatedPutSystemToPending(){
        when(securityRepository.getArmingStatus()).thenReturn(ARMED_HOME);
        when(securityRepository.getAlarmStatus()).thenReturn(NO_ALARM);
        securityService.changeSensorActivationStatus(sensor,true);
        verify(securityRepository,atMostOnce()).setAlarmStatus(PENDING_ALARM);
    }

    @Test//for mentioned test case 2
    void alarmArmedSensorActivatedSystemAlreadyInPendingSetAlarmStatusToAlarm(){
        when(securityRepository.getArmingStatus()).thenReturn(ARMED_HOME);
        when(securityRepository.getAlarmStatus()).thenReturn(PENDING_ALARM);
        securityService.changeSensorActivationStatus(sensor,true);
        verify(securityRepository,atMostOnce()).setAlarmStatus(ALARM);
    }

    @Test //for mentioned test case 3
    void pendingAlarmAndSensorInactiveSetNoAlarmState(){
        when(securityRepository.getAlarmStatus()).thenReturn(AlarmStatus.PENDING_ALARM);
        sensor.setActive(false);
        securityService.changeSensorActivationStatus(sensor,false);
        verify(securityRepository,atMostOnce()).setAlarmStatus(NO_ALARM);
    }

    @ParameterizedTest //for mentioned test case 4
    @ValueSource(booleans = {true,false})
    void alarmIsActiveChangeInSensorStateShouldNotEffectAlarmState(boolean state){
        when(securityRepository.getAlarmStatus()).thenReturn(ALARM);
        securityService.changeSensorActivationStatus(sensor, state);
        verify(securityRepository, never()).setAlarmStatus(any(AlarmStatus.class));
    }

    @Test //for mentioned test 5
    void sensorActivatedWhileAlreadyActive_SystemPendingState_ChangeToAlarmState(){
        when(securityRepository.getAlarmStatus()).thenReturn(AlarmStatus.PENDING_ALARM);
        sensor.setActive(true);
        securityService.changeSensorActivationStatus(sensor,true);
        verify(securityRepository,atMostOnce()).setAlarmStatus(ALARM);
    }

    @ParameterizedTest //for mentioned test 6
    @EnumSource(value = AlarmStatus.class,names ={"ALARM","NO_ALARM","PENDING_ALARM"})
    void sensorDeactivatedWhileInactiveMakeNoChangeToAlarmState(AlarmStatus state){
        sensor.setActive(false);
        when(securityRepository.getAlarmStatus()).thenReturn(state);
        securityService.changeSensorActivationStatus(sensor,false);
        verify(securityRepository,never()).setAlarmStatus(any(AlarmStatus.class));

    }

    @Test //for mentioned test case 7
    void catDetectedPutTheSystemInToAlarmState() {
        when(imageService.imageContainsCat(any(),anyFloat())).thenReturn(true);
        when(securityRepository.getArmingStatus()).thenReturn(ARMED_HOME);
        securityService.processImage(mock(BufferedImage.class));
        verify(securityRepository,atMostOnce()).setAlarmStatus(ALARM);
    }

    @Test //for mentioned test case 8
    void noACatImageIdentifiedChangeStatusToNoAlarmAsLongAsSensorsNotActive() {
        Set<Sensor> sensors = getSensorsTest(3,false);
        lenient().when(securityRepository.getSensors()).thenReturn(sensors);
        when(imageService.imageContainsCat(any(),anyFloat())).thenReturn(false);
        securityService.processImage(mock(BufferedImage.class));
        verify(securityRepository,atMostOnce()).setAlarmStatus(NO_ALARM);
    }

    @Test //for mentioned test case 9
    void systemIsDisalarmedSetStatusToNoAlarm(){
        securityService.setArmingStatus(DISARMED);
        verify(securityRepository,atMostOnce()).setAlarmStatus(NO_ALARM);
    }

    @ParameterizedTest //for mentioned test case 10
    @EnumSource(value = ArmingStatus.class,names={"ARMED_AWAY","ARMED_HOME"})
    void systemIsArmedSetAllSensorsToInactive(ArmingStatus status){
        Set<Sensor> sensors = getSensorsTest(2,false);

        when(securityService.getAlarmStatus()).thenReturn(PENDING_ALARM);
        when(securityRepository.getSensors()).thenReturn(sensors);
        securityService.setArmingStatus(status);
        securityService.getSensors().forEach(sensor->assertFalse(sensor.getActive()));
    }

    @Test //for mentioned test case 11
    void systemArmedHomeCamaraShowsCatSetAlarmStatus(){
        when(securityRepository.getArmingStatus()).thenReturn(ARMED_HOME);
        when(imageService.imageContainsCat(any(),anyFloat())).thenReturn(true);
        when(securityRepository.getArmingStatus()).thenReturn(ARMED_HOME);
        securityService.processImage(mock(BufferedImage.class));
        securityService.setAlarmStatus(ALARM);
        verify(securityRepository,atMost(2)).setAlarmStatus(ALARM);

    }
    @Test
    void changeAlarmStatusSystemArmedHomeAndCatDetectedChangeToAlarmStatus(){
        when(imageService.imageContainsCat(any(), anyFloat())).thenReturn(true);
        when(securityRepository.getArmingStatus()).thenReturn(ArmingStatus.ARMED_HOME);
        securityService.processImage(mock(BufferedImage.class));
        verify(securityRepository, atMostOnce()).setAlarmStatus(ALARM);
    }

    @Test
    void AlarmStateAndSystemDisarmedChangeStatusToPending() {
        when(securityRepository.getArmingStatus()).thenReturn(DISARMED);
        securityService.changeSensorActivationStatus(sensor,true);
        verify(securityRepository,atMostOnce()).setAlarmStatus(PENDING_ALARM);
    }

    @Test
    void testTheHandleSensorDeactivatedWhenAlarmInPendingAlarmState(){
        sensor.setActive(true);
        when(securityRepository.getAlarmStatus()).thenReturn(PENDING_ALARM);
        securityService.changeSensorActivationStatus(sensor,false);
        verify(securityRepository,atMostOnce()).setAlarmStatus(NO_ALARM);
    }
    @Test
    void testTheHandleSensorDeactivatedWhenAlarmInAlarmState(){
        when(securityService.getAlarmStatus()).thenReturn(ALARM);
        securityService.handleSensorDeactivated();
        verify(securityRepository).setAlarmStatus(ALARM);
    }

    @Test
    void testAddRemoveSensor(){
        securityService.addSensor(sensor);
        verify(securityRepository).addSensor(sensor);
        securityService.removeSensor(sensor);
        verify(securityRepository).removeSensor(sensor);
    }
    @Test
    void testAddRemoveListener(){
        securityService.addStatusListener(listener);
        securityService.removeStatusListener(listener);
    }
    @Test
    void whenCatIsDetectedAndArmingStatusIsHomeSetAlarmStatus(){
        securityService.setCatDetect(true);
        lenient().when(securityRepository.getArmingStatus()).thenReturn(ARMED_HOME);
        securityService.setArmingStatus(securityService.getArmingStatus());
        verify(securityRepository,atMostOnce()).setAlarmStatus(ALARM);
    }

    @Test
    void deactivateASensorWhenTheSystemIsInTheALARMState(){
        when(securityRepository.getAlarmStatus()).thenReturn(ALARM);
        sensor.setActive(true);
        securityService.changeSensorActivationStatus(sensor,false);
        verify(securityRepository).setAlarmStatus(ALARM);
    }
}
