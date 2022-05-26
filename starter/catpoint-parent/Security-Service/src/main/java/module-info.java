module Security.Service {
    exports com.udacity.securityService.security to application;
    exports com.udacity.securityService.data to  application;
    exports com.udacity.securityService.application to application;
    requires Image.Service;
    requires java.desktop;
    requires miglayout;
    requires java.prefs;
    requires com.google.gson;
    requires com.google.common;
    opens com.udacity.securityService.data to com.google.gson;
    //opens com.udacity.securityService.security to org.mockito;
}